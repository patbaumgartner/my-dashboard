<script lang="ts">
  import { onMount } from 'svelte'
  import { ArrowLeft, Check, ExternalLink, RefreshCw } from '@lucide/svelte'

  interface ServiceSummary {
    id: string
    name: string
    url: string
  }

  interface AvailabilityResponse {
    serviceId: string
    name: string
    url: string
    state: string
    available: boolean
    availability: string
    startupTimeoutSeconds: number
  }

  type GateStatus = 'loading' | 'waiting' | 'ready' | 'timeout' | 'stopped' | 'missing' | 'error'

  const POLL_INTERVAL_MS = 2_000
  const CONFIRM_INTERVAL_MS = 1_000
  const STOPPED_POLL_INTERVAL_MS = 5_000
  const DEFAULT_TIMEOUT_SECONDS = 120
  // A service that was already up when the page opened only needs one
  // confirmation; one that is still booting must pass a few checks in a row,
  // because Traefik can briefly answer before the route settles.
  const PASSES_WHEN_ALREADY_UP = 2
  const PASSES_WHILE_BOOTING = 3
  const STOPPED_READS_BEFORE_GIVING_UP = 2

  let service: ServiceSummary | null = null
  let status: GateStatus = 'loading'
  let elapsedSeconds = 0
  let lastCheck = ''
  let statusNote = ''

  let elapsedTimer = 0
  let pollTimer = 0
  let redirectTimer = 0
  let startedAt = 0
  let deadlineAt = 0
  let consecutivePasses = 0
  let consecutiveStopped = 0
  let hasFailedCheck = false
  let disposed = false
  let checking = false
  let timeoutSeconds = DEFAULT_TIMEOUT_SECONDS

  $: formattedElapsed = `${String(Math.floor(elapsedSeconds / 60)).padStart(2, '0')}:${String(elapsedSeconds % 60).padStart(2, '0')}`
  $: animating = status === 'loading' || status === 'waiting'

  onMount(() => {
    void start()

    return () => {
      disposed = true
      window.clearInterval(elapsedTimer)
      window.clearTimeout(pollTimer)
      window.clearTimeout(redirectTimer)
    }
  })

  async function fetchAvailability(serviceID: string): Promise<AvailabilityResponse> {
    const response = await fetch(
      `${import.meta.env.BASE_URL}api/services/${encodeURIComponent(serviceID)}/availability`,
      { headers: { Accept: 'application/json' }, cache: 'no-store' },
    )
    if (response.status === 404) throw new ServiceMissingError()
    if (!response.ok) throw new Error(`Check failed (${response.status})`)
    return (await response.json()) as AvailabilityResponse
  }

  class ServiceMissingError extends Error {}

  async function start() {
    const serviceID = new URLSearchParams(window.location.search).get('service') ?? ''
    if (!serviceID) {
      status = 'missing'
      return
    }

    let first: AvailabilityResponse
    try {
      first = await fetchAvailability(serviceID)
    } catch (error) {
      if (disposed) return
      if (error instanceof ServiceMissingError) {
        status = 'missing'
        return
      }
      lastCheck = error instanceof Error ? error.message : 'Status request failed'
      status = 'error'
      return
    }
    if (disposed) return
    if (!first.url) {
      status = 'missing'
      return
    }

    service = { id: first.serviceId || serviceID, name: first.name || serviceID, url: first.url }
    timeoutSeconds = first.startupTimeoutSeconds || DEFAULT_TIMEOUT_SECONDS
    startedAt = Date.now()
    deadlineAt = startedAt + timeoutSeconds * 1_000
    status = 'waiting'
    elapsedTimer = window.setInterval(() => {
      elapsedSeconds = Math.floor((Date.now() - startedAt) / 1_000)
      if (status === 'waiting' && Date.now() >= deadlineAt) {
        status = 'timeout'
        consecutivePasses = 0
        window.clearTimeout(pollTimer)
      }
    }, 500)
    applyCheck(first)
    scheduleNextCheck()
  }

  function applyCheck(payload: AvailabilityResponse) {
    if (!service) return
    if (payload.url) service = { ...service, url: payload.url }

    if (payload.state === 'stopped') {
      consecutivePasses = 0
      hasFailedCheck = true
      consecutiveStopped++
      lastCheck = payload.availability
      if (consecutiveStopped >= STOPPED_READS_BEFORE_GIVING_UP) status = 'stopped'
      return
    }
    consecutiveStopped = 0
    if (status === 'stopped') {
      // Someone started it after all; give it a fresh startup budget.
      status = 'waiting'
      deadlineAt = Date.now() + timeoutSeconds * 1_000
    }

    if (Date.now() >= deadlineAt) {
      status = 'timeout'
      return
    }
    if (!payload.available) {
      consecutivePasses = 0
      hasFailedCheck = true
      lastCheck = payload.availability
      return
    }
    consecutivePasses++
    const required = hasFailedCheck ? PASSES_WHILE_BOOTING : PASSES_WHEN_ALREADY_UP
    if (consecutivePasses >= required) {
      status = 'ready'
      statusNote = payload.availability
      redirectTimer = window.setTimeout(() => void openService(), 900)
    }
  }

  function scheduleNextCheck() {
    if (disposed || (status !== 'waiting' && status !== 'stopped')) return
    window.clearTimeout(pollTimer)
    const delay =
      status === 'stopped'
        ? STOPPED_POLL_INTERVAL_MS
        : consecutivePasses > 0 && !hasFailedCheck
          ? CONFIRM_INTERVAL_MS
          : POLL_INTERVAL_MS
    pollTimer = window.setTimeout(() => void pollAvailability(), delay)
  }

  async function pollAvailability() {
    if (!service || (status !== 'waiting' && status !== 'stopped') || disposed || checking) return
    checking = true
    try {
      const payload = await fetchAvailability(service.id)
      if (disposed || (status !== 'waiting' && status !== 'stopped')) return
      applyCheck(payload)
    } catch (error) {
      consecutivePasses = 0
      hasFailedCheck = true
      lastCheck = error instanceof Error ? error.message : 'Check request failed'
    } finally {
      checking = false
      scheduleNextCheck()
    }
  }

  async function openService() {
    if (!service?.url || status !== 'ready' || disposed || checking) return
    checking = true
    window.clearTimeout(redirectTimer)
    try {
      const payload = await fetchAvailability(service.id)
      if (disposed) return
      if (payload.available && payload.url) {
        window.location.assign(payload.url)
        return
      }
      lastCheck = payload.availability
    } catch (error) {
      lastCheck = error instanceof Error ? error.message : 'Check request failed'
    } finally {
      checking = false
    }
    if (disposed) return
    keepWaiting()
  }

  function keepWaiting() {
    window.clearTimeout(pollTimer)
    window.clearTimeout(redirectTimer)
    status = 'waiting'
    consecutivePasses = 0
    hasFailedCheck = true
    deadlineAt = Date.now() + timeoutSeconds * 1_000
    void pollAvailability()
  }

  function backToDashboard() {
    window.location.assign(import.meta.env.BASE_URL)
  }
</script>

<svelte:head>
  <title>Starting · myDashboard</title>
</svelte:head>

<main class="startup-gate" class:is-animating={animating}>
  <div class="gate-card">
    <svg class="gate-logo" viewBox="0 0 400 400" role="img" aria-label="myDashboard logo">
      <rect width="400" height="400" rx="56" fill="#00AFE1" />
      <g class="gate-tree">
        <path
          fill="#FFFFFF"
          d="M198.7,100.2c-5,1-3.1-1.7-31.2,43.8c-8.4,13.6-20.6,33.3-22.5,36.4c-1.1,1.7-7.1,11.5-13.4,21.7
            c-6.3,10.2-13.6,22-16.2,26.2c-5.8,9.4-7,11.5-7.6,14.5c-1.1,5.4,1.8,8.9,8.2,9.8c3.6,0.5,49.8,0.5,53.8,0c7.8-1,11.1-6.2,7-10.9
            c-2.5-2.8-2.7-2.9-22.9-3.2c-27.9-0.4-27.7-0.1-14.5-21.2c5.9-9.5,10.3-16.6,26.8-43.2c4.4-7.2,10.1-16.3,12.5-20.2
            c22.4-36.4,20.5-35.5,33.1-15.2c3.4,5.5,9.9,16,14.4,23.3c4.5,7.3,10.3,16.6,12.8,20.7c2.5,4.1,7.4,11.9,10.7,17.4
            c3.4,5.5,8.3,13.4,10.9,17.6c13,20.9,13.2,20.7-14.6,21.1c-20.2,0.3-20.4,0.3-22.9,3.2c-2.8,3.2-2,7.2,1.9,9.5
            c2.9,1.7,6.9,1.9,34.6,1.7l25.3-0.1l2.4-1c7.1-3,6.9-8.2-0.7-20.3c-3.9-6.2-7-11.3-16.3-26.2c-4.5-7.3-9.3-15.1-10.7-17.4
            c-1.4-2.3-5.4-8.8-9-14.5c-16.1-26-21.9-35.5-27.6-44.6c-2-3.2-5.3-8.6-7.4-12C206,101.2,203.9,99.2,198.7,100.2 M197.1,213.8
            c-1,0.5-1.8,1.5-2.7,3.2l-1.2,2.5v29.1c0,31.7,0,31,2.3,33.7c3.4,3.9,9.2,2.2,11-3.3c0.9-2.6,1.4-52.2,0.6-57.4
            C205.9,214.6,201.8,211.3,197.1,213.8"
        />
      </g>
      <path
        fill="#FFFFFF"
        d="M125,268h150c8.837,0,16,7.163,16,16s-7.163,16-16,16H125c-8.837,0-16-7.163-16-16
          S116.163,268,125,268z"
      />
    </svg>

    {#if status === 'error'}
      <p class="eyebrow">myDashboard</p>
      <h1>Status unavailable</h1>
      <p class="gate-copy" role="alert">{lastCheck}</p>
      <div class="gate-actions">
        <button class="gate-primary" type="button" onclick={backToDashboard}>
          <ArrowLeft size={15} /> Back to dashboard
        </button>
      </div>
    {:else if status === 'missing'}
      <p class="eyebrow">myDashboard</p>
      <h1>Service not found</h1>
      <p class="gate-copy">
        This waiting page does not know the requested service. Head back to the dashboard and
        open it from there.
      </p>
      <div class="gate-actions">
        <button class="gate-primary" type="button" onclick={backToDashboard}>
          <ArrowLeft size={15} /> Back to dashboard
        </button>
      </div>
    {:else if service}
      <p class="eyebrow">myDashboard</p>
      <h1>
        {#if status === 'ready'}Opening {service.name}…
        {:else if status === 'timeout'}{service.name} is taking a while
        {:else if status === 'stopped'}{service.name} is stopped
        {:else}Starting {service.name}…{/if}
      </h1>

      {#if status !== 'stopped'}
        <p class="gate-counter" class:is-spinning={animating} aria-hidden="true">
          {#if status === 'ready'}<Check size={15} />{:else}<RefreshCw size={15} />{/if}
          <span>{formattedElapsed}</span>
        </p>
      {/if}

      <p class="gate-copy" role="status">
        {#if status === 'stopped'}
          Start it from the dashboard. This page keeps watching and opens it as soon as it is
          up.
        {:else if status === 'ready'}
          {service.name} is ready ({statusNote || 'route reachable'}). Opening now…
        {:else if status === 'timeout'}
          Still not reachable after {formattedElapsed}. The container may need more time to boot.
        {:else if status === 'loading'}
          Looking up {service.name}…
        {:else}
          Waiting for {service.name} to answer{lastCheck ? ` — last check: ${lastCheck}` : '…'}
        {/if}
      </p>

      {#if status === 'stopped'}
        <div class="gate-actions">
          <button class="gate-primary" type="button" onclick={backToDashboard}>
            <ArrowLeft size={15} /> Back to dashboard
          </button>
        </div>
      {:else if status === 'timeout'}
        <div class="gate-actions">
          <button class="gate-primary" type="button" onclick={keepWaiting}>
            <RefreshCw size={15} /> Keep waiting
          </button>
          <button class="gate-secondary" type="button" onclick={backToDashboard}>
            <ArrowLeft size={14} /> Dashboard
          </button>
        </div>
      {:else if status === 'ready'}
        <div class="gate-actions">
          <button class="gate-primary" type="button" onclick={openService}>
            Open {service.name} now <ExternalLink size={14} />
          </button>
        </div>
      {/if}
    {:else}
      <p class="eyebrow">myDashboard</p>
      <h1>Preparing…</h1>
      <p class="gate-copy">Reading the homelab state…</p>
    {/if}
  </div>
</main>
