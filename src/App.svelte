<script lang="ts">
  import '@fontsource-variable/manrope'
  import {
    Activity,
    Check,
    ChevronRight,
    CircleAlert,
    Clock3,
    ExternalLink,
    HardDrive,
    Lock,
    Moon,
    Network,
    Play,
    RefreshCw,
    RotateCw,
    Router,
    Search,
    Server,
    Square,
    Sun,
    X,
  } from '@lucide/svelte'
  import { onMount, tick } from 'svelte'
  import StartupGate from './StartupGate.svelte'
  import { followSystemTheme, preferredTheme, saveTheme, type Theme } from './theme'

  type Lifecycle = 'always-on' | 'scheduled' | 'on-demand' | 'discovered'
  type Category = 'infrastructure' | 'monitoring' | 'media' | 'tools' | 'automation' | 'discovered'
  type ServiceState = 'healthy' | 'running' | 'degraded' | 'stopped' | 'unknown'
  type ServiceAction = 'start' | 'stop' | 'restart'

  interface ContainerState {
    name: string
    image: string
    state: string
    status: string
    health: string
  }

  interface Service {
    id: string
    name: string
    description: string
    icon: string
    url: string
    project: string
    category: Category
    lifecycle: Lifecycle
    schedule: string
    windowStart: string
    windowEnd: string
    state: ServiceState
    status: string
    available: boolean
    availability: string
    startupTimeoutSeconds: number
    controlsLocked: boolean
    availableActions: ServiceAction[]
    containers: ContainerState[]
  }

  interface QuickLink {
    id: string
    name: string
    description: string
    url: string
    icon: string
  }

  interface FooterConfig {
    year: number
    name: string
    url: string
    message: string
  }

  interface LifecycleFilter {
    id: 'all' | Lifecycle
    label: string
    shortLabel: string
  }

  interface CategoryConfig {
    id: Category
    title: string
    description: string
  }

  interface AvailabilityResponse {
    serviceId: string
    state: ServiceState
    available: boolean
    availability: string
  }

  interface SiteConfig {
    name: string
    subtitle: string
    description: string
    timeZone: string
  }

  interface ServicesResponse {
    site?: SiteConfig
    lifecycleFilters: LifecycleFilter[]
    categories: CategoryConfig[]
    quickLinks: QuickLink[]
    footer: FooterConfig
    services: Service[]
    updatedAt: string
    dockerConnected: boolean
    error?: string
  }

  let services: Service[] = []
  let lifecycleOptions: LifecycleFilter[] = []
  let categories: CategoryConfig[] = []
  let quickLinks: QuickLink[] = []
  let footer: FooterConfig | null = null
  let site: SiteConfig = { name: 'myDashboard', subtitle: '', description: '', timeZone: '' }
  let query = ''
  let activeLifecycle: 'all' | Lifecycle = 'all'
  let loading = true
  let refreshing = false
  let dockerConnected = false
  let errorMessage = ''
  let actionError = ''
  let actionMessage = ''
  let actionInFlight = ''
  let actionInFlightType: ServiceAction | '' = ''
  let updatedAt = ''
  let selectedService: Service | null = null
  let detailsDialog: HTMLDialogElement
  let confirmDialog: HTMLDialogElement
  let pendingAction: { service: Service; action: ServiceAction } | null = null
  let theme: Theme = 'light'
  let failedIcons: Record<string, boolean> = {}
  let refreshAnnouncement = ''
  let refreshSucceeded = false
  let requestSequence = 0
  let appliedSequence = 0
  let refreshFeedbackTimer = 0
  let actionMessageTimer = 0
  let view: 'dashboard' | 'gate' = 'dashboard'

  $: normalizedQuery = query.trim().toLowerCase()
  $: filteredServices = services.filter((service) => {
    const lifecycleMatches =
      activeLifecycle === 'all' || service.lifecycle === activeLifecycle
    const searchable = [
      service.name,
      service.description,
      service.project,
      service.category,
      ...service.containers.map((container) => container.name),
    ]
      .join(' ')
      .toLowerCase()

    return (
      lifecycleMatches &&
      (!normalizedQuery || searchable.includes(normalizedQuery))
    )
  })
  $: visibleCategories = categories.filter((category) =>
    filteredServices.some((service) => service.category === category.id),
  )
  $: operationalCount = services.filter(isInExpectedState).length
  $: attentionCount = services.filter(
    (service) => service.state !== 'unknown' && !isInExpectedState(service),
  ).length
  $: formattedUpdateText = refreshing
    ? 'Refreshing containers and routes…'
    : updatedAt
      ? `Updated ${new Intl.DateTimeFormat('en-CH', {
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
        }).format(new Date(updatedAt))}`
      : 'Waiting for status'

  onMount(() => {
    if (window.location.pathname.replace(/\/+$/, '') === '/starting') {
      view = 'gate'
      return
    }
    theme = preferredTheme()
    const stopFollowingSystem = followSystemTheme((systemTheme) => (theme = systemTheme))

    void loadServices()
    const interval = window.setInterval(() => {
      if (document.visibilityState === 'visible') void loadServices(true)
    }, 15_000)
    const refreshOnFocus = () => {
      if (document.visibilityState === 'visible') void loadServices(true)
    }
    document.addEventListener('visibilitychange', refreshOnFocus)

    return () => {
      window.clearInterval(interval)
      window.clearTimeout(refreshFeedbackTimer)
      window.clearTimeout(actionMessageTimer)
      document.removeEventListener('visibilitychange', refreshOnFocus)
      stopFollowingSystem()
    }
  })

  async function loadServices(background = false): Promise<ServicesResponse | null> {
    const sequence = ++requestSequence
    if (background) refreshing = true
    else loading = true

    try {
      const response = await fetch(`${import.meta.env.BASE_URL}api/services`, {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      })
      if (!response.ok) {
        throw new Error(`Status request failed (${response.status})`)
      }

      const payload = (await response.json()) as ServicesResponse
      if (sequence >= appliedSequence) {
        appliedSequence = sequence
        applyServicesResponse(payload)
      }
      return payload
    } catch (error) {
      if (sequence >= appliedSequence) {
        errorMessage =
          error instanceof Error ? error.message : 'Status request failed'
        dockerConnected = false
      }
      return null
    } finally {
      if (sequence === requestSequence) {
        loading = false
        refreshing = false
      }
    }
  }

  function applyServicesResponse(payload: ServicesResponse) {
    services = payload.services
    lifecycleOptions = payload.lifecycleFilters ?? []
    categories = payload.categories ?? []
    quickLinks = payload.quickLinks ?? []
    footer = payload.footer ?? null
    if (payload.site) site = payload.site
    if (selectedService) {
      const current = services.find((service) => service.id === selectedService?.id)
      if (current) selectedService = current
      else if (detailsDialog?.open) detailsDialog.close()
      else selectedService = null
    }
    dockerConnected = payload.dockerConnected
    errorMessage = payload.error ?? ''
    updatedAt = payload.updatedAt
  }

  async function refreshServices() {
    if (refreshing) return
    refreshAnnouncement = ''
    refreshSucceeded = false
    const payload = await loadServices(true)
    if (!payload) {
      refreshAnnouncement = 'Refresh failed. Existing service data was kept.'
      return
    }
    refreshSucceeded = true
    refreshAnnouncement = `Containers and routes refreshed at ${new Intl.DateTimeFormat('en-CH', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(new Date(payload.updatedAt))}`
    window.clearTimeout(refreshFeedbackTimer)
    refreshFeedbackTimer = window.setTimeout(() => {
      refreshSucceeded = false
    }, 1800)
  }

  function toggleTheme() {
    theme = theme === 'dark' ? 'light' : 'dark'
    saveTheme(theme)
  }

  // Clicks on the dialog's own padding also target the dialog element, so only
  // treat a click as a backdrop click when it lands outside the dialog box.
  function isBackdropClick(event: MouseEvent, dialog: HTMLDialogElement) {
    if (event.target !== dialog) return false
    const box = dialog.getBoundingClientRect()
    return (
      event.clientX < box.left ||
      event.clientX > box.right ||
      event.clientY < box.top ||
      event.clientY > box.bottom
    )
  }

  function openDetails(service: Service) {
    selectedService = service
    requestAnimationFrame(() => detailsDialog.showModal())
  }

  function closeDetails() {
    detailsDialog.close()
  }

  function markIconFailed(serviceID: string) {
    failedIcons = { ...failedIcons, [serviceID]: true }
  }

  function requestAction(service: Service, action: ServiceAction) {
    if (!service.availableActions.includes(action) || actionInFlight) return
    if (detailsDialog?.open) detailsDialog.close()
    pendingAction = { service, action }
    requestAnimationFrame(() => confirmDialog.showModal())
  }

  function closeConfirmation() {
    confirmDialog.close()
    pendingAction = null
  }

  async function executeAction() {
    if (!pendingAction) return
    const { service, action } = pendingAction
    confirmDialog.close()
    pendingAction = null
    actionInFlight = service.id
    actionInFlightType = action
    actionError = ''
    actionMessage = ''
    window.clearTimeout(actionMessageTimer)

    try {
      const response = await fetch(
        `${import.meta.env.BASE_URL}api/services/${encodeURIComponent(service.id)}/actions/${action}`,
        {
          method: 'POST',
          headers: { 'X-MyDashboard-Action': 'confirm' },
          cache: 'no-store',
        },
      )
      if (!response.ok) {
        const message = (await response.text()).trim()
        throw new Error(message || `${actionLabel(action)} failed (${response.status})`)
      }
      await waitForActionState(service, action)
      actionMessage = action === 'stop'
        ? `${service.name} stopped.`
        : `${service.name} is ready.`
      actionMessageTimer = window.setTimeout(() => (actionMessage = ''), 8_000)
    } catch (error) {
      actionError = error instanceof Error ? error.message : `${actionLabel(action)} failed`
    } finally {
      await loadServices(true)
      actionInFlight = ''
      actionInFlightType = ''
      await restorePowerControlFocus(service.id)
    }
  }

  // The per-service check is much cheaper than re-reading every route, and it
  // leaves the global refresh indicator alone while an action is in progress.
  async function waitForActionState(service: Service, action: ServiceAction) {
    const timeoutSeconds = action === 'stop' ? 60 : service.startupTimeoutSeconds || 120
    const deadline = Date.now() + timeoutSeconds * 1_000
    while (Date.now() < deadline) {
      const current = await fetchAvailability(service.id)
      if (current && actionReachedExpectedState(current, action)) return
      await new Promise<void>((resolve) => window.setTimeout(resolve, 1_500))
    }

    const expected = action === 'stop' ? 'stop' : 'become ready'
    throw new Error(`${service.name} did not ${expected} within ${timeoutSeconds} seconds`)
  }

  async function fetchAvailability(serviceID: string): Promise<AvailabilityResponse | null> {
    try {
      const response = await fetch(
        `${import.meta.env.BASE_URL}api/services/${encodeURIComponent(serviceID)}/availability`,
        { headers: { Accept: 'application/json' }, cache: 'no-store' },
      )
      return response.ok ? ((await response.json()) as AvailabilityResponse) : null
    } catch {
      return null
    }
  }

  function actionReachedExpectedState(current: AvailabilityResponse, action: ServiceAction) {
    if (action === 'stop') return current.state === 'stopped'
    return current.available && ['healthy', 'running'].includes(current.state)
  }

  // The Start and Stop buttons are different elements, so focus would otherwise
  // fall back to the page body once the action settles.
  async function restorePowerControlFocus(serviceID: string) {
    await tick()
    const active = document.activeElement
    if (active && active !== document.body && document.contains(active) && !(active as HTMLButtonElement).disabled) return
    document
      .querySelector<HTMLElement>(`[data-power-control="${CSS.escape(serviceID)}"]`)
      ?.focus()
  }

  function actionLabel(action: ServiceAction) {
    return action.charAt(0).toUpperCase() + action.slice(1)
  }

  function actionProgressLabel() {
    if (actionInFlightType === 'start') return 'Starting'
    if (actionInFlightType === 'stop') return 'Stopping'
    if (actionInFlightType === 'restart') return 'Restarting'
    return 'Working'
  }

  function canOpen(service: Service, activeAction: string) {
    return activeAction !== service.id && serviceIsReady(service)
  }

  function startupGateHref(service: Service) {
    return `${import.meta.env.BASE_URL}starting?service=${encodeURIComponent(service.id)}`
  }

  function serviceIsReady(service: Service) {
    return Boolean(service.url) && service.available && ['healthy', 'running'].includes(service.state)
  }

  function quickLinkIcon(icon: string) {
    if (icon === 'router') return Router
    if (icon === 'hard-drive') return HardDrive
    if (icon === 'network') return Network
    return ExternalLink
  }

  function servicesFor(category: Category) {
    return filteredServices.filter((service) => service.category === category)
  }

  function stateLabel(service: Service) {
    if (service.lifecycle === 'scheduled' && !scheduleIsActive(service) && service.state === 'stopped') return 'Scheduled off'
    if (service.lifecycle === 'on-demand' && service.state === 'stopped') return 'Ready'
    if (service.state === 'healthy') return 'Healthy'
    if (service.state === 'running') return 'Running'
    if (service.state === 'degraded') return 'Needs attention'
    if (service.state === 'stopped') return 'Stopped'
    return 'Unknown'
  }

  function stateTone(service: Service) {
    if (service.state === 'stopped' && isInExpectedState(service)) return 'idle'
    return service.state
  }

  function isInExpectedState(service: Service) {
    if (service.state === 'degraded' || service.state === 'unknown') return false
    const running = service.state === 'healthy' || service.state === 'running'
    if (service.lifecycle === 'always-on' || service.lifecycle === 'discovered') return running
    if (service.lifecycle === 'on-demand') return running || service.state === 'stopped'
    return scheduleIsActive(service) ? running : service.state === 'stopped'
  }

  function scheduleIsActive(service: Service) {
    if (!service.windowStart || !service.windowEnd) return false
    const parts = new Intl.DateTimeFormat('en-GB', {
      timeZone: site.timeZone || undefined,
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    }).formatToParts(new Date())
    const hour = Number(parts.find((part) => part.type === 'hour')?.value ?? 0)
    const minute = Number(parts.find((part) => part.type === 'minute')?.value ?? 0)
    const current = hour * 60 + minute
    const [startHour, startMinute] = service.windowStart.split(':').map(Number)
    const [endHour, endMinute] = service.windowEnd.split(':').map(Number)
    const start = startHour * 60 + startMinute
    const end = endHour * 60 + endMinute
    if (start <= end) return current >= start && current < end
    return current >= start || current < end
  }

</script>

<svelte:head>
  {#if view !== 'gate'}
    <title>{site.subtitle ? `${site.name} · ${site.subtitle}` : site.name}</title>
  {/if}
  <meta
    name="description"
    content={site.description || 'A calm operational dashboard for your homelab.'}
  />
</svelte:head>

{#if view === 'gate'}
  <StartupGate />
{:else}
<a class="skip-link" href="#dashboard-content">Skip to services</a>

<header class="app-header">
  <div class="header-inner">
    <div class="brand-block">
      <div class="brand-mark">
        <img src={`${import.meta.env.BASE_URL}logo.svg`} alt="" />
      </div>
      <div>
        <p class="product-name">{site.name}</p>
        {#if site.subtitle}
          <p class="product-location">{site.subtitle}</p>
        {/if}
      </div>
    </div>

    <div class="header-actions">
      <div
        class:disconnected={!dockerConnected}
        class="connection-state"
        role="img"
        aria-label={dockerConnected ? 'Docker connected' : 'Status degraded'}
        title={dockerConnected ? 'Docker connected' : 'Status degraded'}
      >
        <span class="connection-dot" aria-hidden="true"></span>
        <span class="connection-label" aria-hidden="true">{dockerConnected ? 'Docker connected' : 'Status degraded'}</span>
      </div>
      <button
        class="icon-button"
        type="button"
        title={theme === 'dark'
          ? 'Use light appearance'
          : 'Use dark appearance'}
        aria-label={theme === 'dark'
          ? 'Use light appearance'
          : 'Use dark appearance'}
        onclick={toggleTheme}
      >
        {#if theme === 'dark'}<Sun size={19} />{:else}<Moon size={19} />{/if}
      </button>
      <button
        class="icon-button"
        class:spinning={refreshing}
        type="button"
        title="Refresh containers and routes"
        aria-label="Refresh containers and routes"
        aria-disabled={refreshing}
        onclick={refreshServices}
      >
        {#if refreshSucceeded}<Check size={19} />{:else}<RefreshCw size={19} />{/if}
      </button>
    </div>
  </div>
</header>

<main id="dashboard-content" class="dashboard-shell">
  <section class="overview" aria-labelledby="overview-title">
    <div>
      <p class="eyebrow">Home infrastructure</p>
      <h1 id="overview-title">
        Good {new Date().getHours() < 12
          ? 'morning'
          : new Date().getHours() < 18
            ? 'afternoon'
            : 'evening'}.
      </h1>
      <p class="overview-copy">
        {#if loading}
          Reading the homelab state…
        {:else if !dockerConnected}
          Live container status is temporarily unavailable.
        {:else if attentionCount > 0}
          {attentionCount} {attentionCount === 1
            ? 'service needs'
            : 'services need'} a closer look.
        {:else}
          All classified services are in their expected state.
        {/if}
      </p>
    </div>

    <dl class="status-summary">
      <div>
        <dt>Expected</dt>
        <dd>{dockerConnected ? operationalCount : '—'}<span>/{services.length || '—'}</span></dd>
      </div>
      <div>
        <dt>Attention</dt>
        <dd class:has-attention={attentionCount > 0}>{attentionCount}</dd>
      </div>
      <div>
        <dt>Inventory</dt>
        <dd>
          {dockerConnected
            ? services.reduce(
                (total, service) => total + service.containers.length,
                0,
              )
            : '—'}<span class="unit-word">containers</span>
        </dd>
      </div>
    </dl>
  </section>

  {#if errorMessage || actionError || actionMessage}
    <div class:error-notice={Boolean(actionError)} class:success-notice={Boolean(actionMessage)} class="notice">
      {#if actionMessage && !actionError}<Check size={18} aria-hidden="true" />{:else}<CircleAlert size={18} aria-hidden="true" />{/if}
      <span>
        {#if actionError}
          {actionError}
        {:else if actionMessage}
          {actionMessage}
        {:else}
          {errorMessage}. Showing the configured inventory with available state.
        {/if}
      </span>
    </div>
  {/if}

  <nav class="quick-access" aria-label="Quick access">
    <div class="quick-access-heading">
      <div>
        <p class="eyebrow">Quick access</p>
        <h2>Home controls</h2>
      </div>
    </div>
    <ul>
      {#each quickLinks as link}
        {@const LinkIcon = quickLinkIcon(link.icon)}
        <li>
          <a href={link.url} target="_blank" rel="noopener noreferrer">
            <span class="quick-link-icon"><LinkIcon size={21} /></span>
            <span>
              <strong>{link.name}</strong>
              <small>{link.description}</small>
            </span>
            <ExternalLink size={15} aria-hidden="true" />
          </a>
        </li>
      {/each}
    </ul>
  </nav>

  <section class="service-browser" aria-labelledby="services-title" aria-busy={refreshing}>
    <div class="browser-heading">
      <div>
        <h2 id="services-title">Services</h2>
        <p>{formattedUpdateText}</p>
      </div>

      <label class="search-field">
        <span class="sr-only">Search services and containers</span>
        <Search size={18} aria-hidden="true" />
        <input bind:value={query} type="search" placeholder="Search services" />
        {#if query}
          <button
            type="button"
            title="Clear search"
            aria-label="Clear search"
            onclick={() => (query = '')}
          ><X size={16} /></button>
        {/if}
      </label>
    </div>

    <div class="segmented-control" role="group" aria-label="Filter services by lifecycle">
      {#each lifecycleOptions as option}
        <button
          type="button"
          class:active={activeLifecycle === option.id}
          aria-pressed={activeLifecycle === option.id}
          onclick={() => (activeLifecycle = option.id)}
        >
          <span class="full-label">{option.label}</span>
          <span class="short-label">{option.shortLabel}</span>
        </button>
      {/each}
    </div>

    {#if loading}
      <div class="loading-list" role="status" aria-label="Loading services">
        {#each Array(6) as _}
          <div class="service-skeleton"></div>
        {/each}
      </div>
    {:else if services.length === 0 && errorMessage}
      <div class="empty-state error-state" role="alert">
        <CircleAlert size={24} />
        <h3>Service status is unavailable</h3>
        <p>The dashboard could not reach its status API.</p>
        <button type="button" onclick={refreshServices}>Try again</button>
      </div>
    {:else if filteredServices.length === 0}
      <div class="empty-state">
        <Search size={24} />
        <h3>No matching services</h3>
        <p>Clear the search or choose another lifecycle.</p>
        <button
          type="button"
          onclick={() => {
            query = ''
            activeLifecycle = 'all'
          }}>Reset filters</button
        >
      </div>
    {:else}
      <div class="category-groups">
        {#each visibleCategories as category}
          <section
            class="category-section"
            aria-labelledby={`${category.id}-title`}
          >
            <div class="category-heading">
              <div>
                <h3 id={`${category.id}-title`}>
                  {category.title}
                </h3>
                <p>{category.description}</p>
              </div>
              <span>{servicesFor(category.id).length}</span>
            </div>

            <ul class="service-grid">
              {#each servicesFor(category.id) as service (service.id)}
                <li class="service-card">
                  <div class="service-identity">
                    <div class="service-icon">
                      {#if service.icon && !failedIcons[service.id]}
                        <img
                          src={service.icon}
                          alt=""
                          loading="lazy"
                          onerror={() => markIconFailed(service.id)}
                        />
                      {:else}
                        <Server size={23} aria-hidden="true" />
                      {/if}
                    </div>
                    <div class="service-copy">
                      <div class="service-title-line">
                        <h4>{service.name}</h4>
                        <span class={`state state-${stateTone(service)}`}>
                          <span aria-hidden="true"></span>{stateLabel(service)}
                        </span>
                      </div>
                      <p>{service.description}</p>
                    </div>
                  </div>

                  <div class="service-meta">
                    {#if service.schedule}
                      <span><Clock3 size={15} />{service.schedule}</span>
                    {:else}
                      <span><Activity size={15} />{service.status}</span>
                    {/if}
                    <span
                      >{service.containers.length} {service.containers.length ===
                      1
                        ? 'container'
                        : 'containers'}</span
                    >
                  </div>

                  <div class="service-actions">
                    {#if service.lifecycle === 'on-demand'}
                      {#if service.availableActions.includes('start')}
                        <button
                          class="power-control"
                          data-power-control={service.id}
                          class:working={actionInFlight === service.id}
                          type="button"
                          disabled={actionInFlight !== ''}
                          onclick={() => requestAction(service, 'start')}
                        >
                          {#if actionInFlight === service.id}<RefreshCw size={17} />{:else}<Play size={17} />{/if}
                          <span>{actionInFlight === service.id ? actionProgressLabel() : 'Start'}</span>
                        </button>
                      {:else if service.availableActions.includes('stop')}
                        <button
                          class="power-control"
                          data-power-control={service.id}
                          class:stop-control={actionInFlight !== service.id}
                          class:working={actionInFlight === service.id}
                          type="button"
                          disabled={actionInFlight !== ''}
                          onclick={() => requestAction(service, 'stop')}
                        >
                          {#if actionInFlight === service.id}<RefreshCw size={17} />{:else}<Square size={16} />{/if}
                          <span>{actionInFlight === service.id ? actionProgressLabel() : 'Stop'}</span>
                        </button>
                      {:else if service.containers.length === 0}
                        <button
                          class="locked-control"
                          type="button"
                          disabled
                          title="No container exists yet. Create it once on the NAS with `docker compose up -d` in this service's directory; after that this becomes a Start button."
                        >
                          <Lock size={14} />
                          <span>Not created</span>
                        </button>
                      {:else}
                        <button class="locked-control" type="button" disabled title="Read-only by policy">
                          <Lock size={14} />
                          <span>Locked</span>
                        </button>
                      {/if}
                    {/if}
                    {#if canOpen(service, actionInFlight)}
                      <a
                        class="icon-button"
                        href={startupGateHref(service)}
                        target="_blank"
                        rel="noopener noreferrer"
                        title={`Open ${service.name}`}
                        aria-label={`Open ${service.name}`}
                      >
                        <ExternalLink size={18} />
                      </a>
                    {/if}
                    <button
                      class="icon-button"
                      type="button"
                      title={`View ${service.name} details`}
                      aria-label={`View ${service.name} details`}
                      onclick={() => openDetails(service)}
                    >
                      <ChevronRight size={19} />
                    </button>
                  </div>
                </li>
              {/each}
            </ul>
          </section>
        {/each}
      </div>
    {/if}
  </section>
</main>

{#if footer}
  <footer class="app-footer">
    <p>
      © {footer.year}
      <a href={footer.url} target="_blank" rel="noopener noreferrer">{footer.name}</a>
      <span class="footer-separator" aria-hidden="true">–</span>
      <span>{footer.message}</span>
    </p>
  </footer>
{/if}

<dialog
  class="details-dialog"
  aria-labelledby="details-title"
  bind:this={detailsDialog}
  onclose={() => (selectedService = null)}
  oncancel={(event) => {
    event.preventDefault()
    closeDetails()
  }}
  onkeydown={(event) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      closeDetails()
    }
  }}
  onclick={(event) => isBackdropClick(event, detailsDialog) && closeDetails()}
>
  {#if selectedService}
    <div class="dialog-header">
      <div>
        <p class="eyebrow">Compose project</p>
        <h2 id="details-title">{selectedService.name}</h2>
        <p>{selectedService.project}</p>
      </div>
      <button
        class="icon-button"
        type="button"
        title="Close details"
        aria-label="Close details"
        onclick={closeDetails}
      >
        <X size={19} />
      </button>
    </div>

    <div class="dialog-status">
      <span class={`state state-${stateTone(selectedService)}`}>
        <span aria-hidden="true"></span>{stateLabel(selectedService)}
      </span>
      <span>{selectedService.status}</span>
    </div>

    {#if selectedService.url}
      <div class="dialog-availability">
        <strong>Web route</strong>
        <span class:available={selectedService.available}>
          {selectedService.available ? 'Available' : selectedService.availability}
        </span>
        {#if canOpen(selectedService, actionInFlight)}
          <a
            href={startupGateHref(selectedService)}
            target="_blank"
            rel="noopener noreferrer"
            aria-label={`Open ${selectedService.name}`}
          >
            Open <ExternalLink size={14} />
          </a>
        {/if}
      </div>
    {/if}

    <div class="dialog-section">
      <h3>Containers</h3>
      {#if selectedService.containers.length > 0}
        <ul class="container-list">
          {#each selectedService.containers as container}
            <li>
              <div>
                <strong>{container.name}</strong>
                <span>{container.image}</span>
              </div>
              <span
                class={`container-state ${container.health === 'unhealthy'
                  ? 'unhealthy'
                  : ''}`}
              >
                {container.health || container.state}
              </span>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="dialog-empty">
          No containers currently exist for this project.
        </p>
      {/if}
    </div>

    {#if selectedService.availableActions.length > 0}
      <div class="control-panel">
        <div>
          <strong>Lifecycle controls</strong>
          <p>Actions apply only to this allowlisted Compose project.</p>
        </div>
        <div class="dialog-actions">
          {#each selectedService.availableActions as action}
            <button
              class:danger-action={action === 'stop'}
              type="button"
              disabled={actionInFlight !== ''}
              onclick={() => requestAction(selectedService!, action)}
            >
              {#if action === 'start'}<Play size={16} />{:else if action === 'stop'}<Square size={15} />{:else}<RotateCw size={16} />{/if}
              {actionLabel(action)}
            </button>
          {/each}
        </div>
      </div>
    {:else}
      <div class="control-boundary">
        <Lock size={18} />
        <div>
          <strong>Read-only by policy</strong>
          <p>This project is visible here but has no dashboard lifecycle actions.</p>
        </div>
      </div>
    {/if}
  {/if}
</dialog>

<dialog
  class="confirm-dialog"
  aria-labelledby="confirm-title"
  aria-describedby="confirm-description"
  bind:this={confirmDialog}
  onclose={() => (pendingAction = null)}
  oncancel={(event) => {
    event.preventDefault()
    closeConfirmation()
  }}
  onkeydown={(event) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      closeConfirmation()
    }
  }}
  onclick={(event) => isBackdropClick(event, confirmDialog) && closeConfirmation()}
>
  {#if pendingAction}
    <div class="confirm-mark" class:stop-mark={pendingAction.action === 'stop'}>
      {#if pendingAction.action === 'start'}<Play size={22} />{:else if pendingAction.action === 'stop'}<Square size={20} />{:else}<RotateCw size={21} />{/if}
    </div>
    <h2 id="confirm-title">{actionLabel(pendingAction.action)} {pendingAction.service.name}?</h2>
    <p id="confirm-description">
      This will {pendingAction.action} the existing container for the
      <strong>{pendingAction.service.project}</strong> project.
    </p>
    <div class="confirm-actions">
      <button type="button" onclick={closeConfirmation}>Cancel</button>
      <button
        class="confirm-primary"
        class:confirm-danger={pendingAction.action === 'stop'}
        type="button"
        onclick={executeAction}
      >
        {actionLabel(pendingAction.action)}
      </button>
    </div>
  {/if}
</dialog>

<div class="sr-only" role="status" aria-live="polite" aria-atomic="true">
  {actionMessage || refreshAnnouncement}
</div>
<div class="sr-only" role="alert" aria-atomic="true">{actionError}</div>
{/if}
