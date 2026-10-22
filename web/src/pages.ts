type Bounty = { id: number; project_id: number; repo_full_name: string; github_issue_number: number; title: string; description: string; difficulty: string; reward_sats: number; net_payout_sats: number; state: string }

const escapeHtml = (value: string) => {
  const element = document.createElement('span')
  element.textContent = value
  return element.innerHTML
}
const formatSats = (value: number) => new Intl.NumberFormat().format(value)
const header = `<header class="site-header"><div class="container nav"><a class="nav-brand" href="/"><span class="brand-mark">₿</span> satlight</a><nav class="nav-links"><a class="nav-link" href="/bounties">Browse bounties</a><a class="btn btn-dark nav-cta" href="/#register">Register a project ↗</a></nav></div></header>`

export function renderExtraPage(app: HTMLDivElement, path: string) {
  if (path === '/bounties') { renderBrowse(app); return true }
  const bountyPath = path.match(/^\/bounties\/([^/]+)$/)
  if (bountyPath) { void renderDetail(app, bountyPath[1]); return true }
  if (path === '/dashboard-preview') { void renderDashboard(app, 'preview', true); return true }
  const dashboardPath = path.match(/^\/dashboard\/(\d+)$/)
  if (dashboardPath) { void renderDashboard(app, dashboardPath[1]); return true }
  return false
}

function renderBrowse(app: HTMLDivElement) {
  app.innerHTML = `${header}<main class="market-page"><section class="container market-content"><p class="eyebrow">OPEN SOURCE, REWARDED</p><div class="market-title-row"><div><h1>Open bounties</h1><p>Pick a task, make a contribution, earn sats.</p></div><span class="market-count" id="bounty-count">Loading bounties…</span></div><form class="filter-bar" id="bounty-filters"><label class="search-field"><span aria-hidden="true">⌕</span><input name="q" placeholder="Search bounties or repositories" aria-label="Search bounties or repositories"></label><select name="language" aria-label="Language"><option value="">All languages</option><option>Go</option><option>TypeScript</option><option>JavaScript</option><option>Python</option><option>Rust</option></select><select name="difficulty" aria-label="Difficulty"><option value="">Any difficulty</option><option value="trivial">Trivial</option><option value="easy">Easy</option><option value="medium">Medium</option><option value="hard">Hard</option></select><select name="sort" aria-label="Sort by"><option value="newest">Newest</option><option value="reward">Highest reward</option></select></form><div class="bounty-grid" id="bounty-results"><p class="market-state">Loading open bounties…</p></div></section></main><footer class="site-footer"><div class="container footer-inner"><a class="nav-brand" href="/"><span class="brand-mark">₿</span> satlight</a><span>Rewarding the work that moves open source forward.</span><a href="/">Back home ↑</a></div></footer>`
  const form = document.querySelector<HTMLFormElement>('#bounty-filters')!
  const results = document.querySelector<HTMLDivElement>('#bounty-results')!
  const count = document.querySelector<HTMLSpanElement>('#bounty-count')!
  let timer = 0
  const load = async () => {
    const params = new URLSearchParams()
    new FormData(form).forEach((value, key) => { if (value && key !== 'sort') params.set(key, String(value)) })
    params.set('limit', '60')
    results.innerHTML = '<p class="market-state">Finding open bounties…</p>'
    try {
      const response = await fetch(`/api/v1/bounties?${params}`)
      if (!response.ok) throw new Error('Bounties are temporarily unavailable.')
      const data = await response.json() as { items?: Bounty[] }
      const items = data.items ?? []
      if ((form.elements.namedItem('sort') as HTMLSelectElement).value === 'reward') items.sort((a, b) => b.reward_sats - a.reward_sats)
      count.textContent = `${items.length} ${items.length === 1 ? 'bounty' : 'bounties'}`
      results.innerHTML = items.length ? items.map((b) => `<article class="market-card"><div class="market-card-meta"><span class="state-dot">● ${escapeHtml(b.state)}</span><span>#${b.github_issue_number}</span></div><a class="market-repo" href="https://github.com/${encodeURIComponent(b.repo_full_name).replace(/%2F/g, '/')}">${escapeHtml(b.repo_full_name)}</a><h2><a href="/bounties/${b.id}">${escapeHtml(b.title)}</a></h2><p>${escapeHtml(b.description.slice(0, 150))}${b.description.length > 150 ? '…' : ''}</p><div class="market-tags"><span>${escapeHtml(b.difficulty)}</span></div><div class="market-card-foot"><strong>⚡ ${formatSats(b.reward_sats)} <small>sats</small></strong><a class="btn btn-dark" href="/bounties/${b.id}">View bounty →</a></div></article>`).join('') : '<div class="market-empty"><span>✳</span><h2>No open bounties yet</h2><p>Try a different search, or check back soon as maintainers add new work.</p></div>'
    } catch (error) { count.textContent = ''; results.innerHTML = `<p class="market-state is-error">${escapeHtml(error instanceof Error ? error.message : 'Could not load bounties.')}</p>` }
  }
  form.addEventListener('input', () => { window.clearTimeout(timer); timer = window.setTimeout(load, 250) })
  form.addEventListener('change', load)
  void load()
}

async function renderDetail(app: HTMLDivElement, id: string) {
  app.innerHTML = `${header}<main class="container detail-page"><a class="breadcrumb" href="/bounties">← All bounties</a><div id="bounty-detail" class="market-state">Loading bounty…</div></main>`
  const target = document.querySelector<HTMLDivElement>('#bounty-detail')!
  try {
    const response = await fetch(`/api/v1/bounties/${encodeURIComponent(id)}`)
    if (!response.ok) throw new Error(response.status === 404 ? 'This bounty could not be found.' : 'Bounty details are temporarily unavailable.')
    const b = await response.json() as Bounty
    target.className = 'detail-layout'
    target.innerHTML = `<article class="detail-main"><div class="market-card-meta"><span class="state-dot">● ${escapeHtml(b.state)}</span><span>${escapeHtml(b.difficulty)}</span><span>Issue #${b.github_issue_number}</span></div><a class="market-repo" href="https://github.com/${encodeURIComponent(b.repo_full_name).replace(/%2F/g, '/')}">${escapeHtml(b.repo_full_name)}</a><h1>${escapeHtml(b.title)}</h1><div class="detail-description">${escapeHtml(b.description).replace(/\n/g, '<br>')}</div><section class="detail-section"><h2>Getting started</h2><p>Review the linked issue and repository contribution guidelines before you begin.</p><a class="text-link" target="_blank" rel="noreferrer" href="https://github.com/${encodeURIComponent(b.repo_full_name).replace(/%2F/g, '/')}/issues/${b.github_issue_number}">Open issue on GitHub ↗</a></section><div class="detail-timeline"><span class="active">Open</span><i></i><span>Claimed</span><i></i><span>In review</span><i></i><span>Paid</span></div></article><aside class="reward-panel"><p class="eyebrow">BOUNTY REWARD</p><strong class="detail-reward">⚡ ${formatSats(b.reward_sats)} <small>sats</small></strong><p class="reward-net">Contributor payout: ${formatSats(b.net_payout_sats)} sats after 2% platform fee.</p><button class="btn btn-primary btn-large" id="claim-bounty" ${b.state !== 'open' ? 'disabled' : ''}>${b.state === 'open' ? 'Claim this bounty →' : 'Bounty is not open'}</button><p class="claim-message" id="claim-message" role="status"></p><p class="expiry-note">Claim expires after 7 days. Sign in with GitHub to claim.</p><hr><p class="lightning-hint">⚡ Add a Lightning address to your account to receive payouts.</p></aside>`
    document.querySelector<HTMLButtonElement>('#claim-bounty')?.addEventListener('click', async (event) => {
      const button = event.currentTarget as HTMLButtonElement
      const message = document.querySelector<HTMLParagraphElement>('#claim-message')!
      button.disabled = true; message.textContent = 'Claiming…'
      try {
        const result = await fetch(`/api/v1/bounties/${b.id}/claim`, { method: 'POST' })
        const data = await result.json().catch(() => ({})) as { error?: { message?: string } }
        if (result.status === 401) { window.location.href = '/api/v1/auth/github'; return }
        if (!result.ok) throw new Error(data.error?.message ?? 'Could not claim this bounty.')
        message.textContent = 'Bounty claimed. Head to GitHub to start working on the issue.'; message.className = 'claim-message is-success'
      } catch (error) { message.textContent = error instanceof Error ? error.message : 'Could not claim this bounty.'; message.className = 'claim-message is-error'; button.disabled = false }
    })
  } catch (error) { target.className = 'market-empty'; target.innerHTML = `<h1>${escapeHtml(error instanceof Error ? error.message : 'Could not load bounty.')}</h1><a class="btn btn-dark" href="/bounties">Browse bounties</a>` }
}

async function renderDashboard(app: HTMLDivElement, projectId: string, preview = false) {
  const repo = new URLSearchParams(window.location.search).get('repo') ?? '[owner/repo]'
  app.innerHTML = `<main class="maintainer-shell"><aside class="maintainer-sidebar"><a class="maintainer-wordmark" href="/">Satlight</a><nav aria-label="Project navigation"><a class="current" href="#overview">Overview</a><a href="#your-bounties">Bounties</a><a href="#fund-form">Wallet</a><a href="/#register">Settings</a></nav></aside><section class="maintainer-main" id="overview">${preview ? '<p class="dashboard-preview-note">Preview mode · example balances and bounties</p>' : ''}<h1 class="maintainer-repo">${escapeHtml(repo)}</h1><div class="maintainer-summary"><article class="maintainer-stat"><span>Available</span><strong id="available-balance">…</strong></article><article class="maintainer-stat"><span>Reserved in escrow</span><strong id="reserved-balance">…</strong></article><article class="maintainer-stat"><span>Paid out</span><strong id="paid-balance">—</strong></article><form class="maintainer-fund" id="fund-form"><div class="qr-placeholder" id="qr-invoice">[QR invoice]</div><button class="btn btn-dark" type="submit">Fund wallet</button><p class="dashboard-message" id="fund-message" role="status"></p></form></div><section class="maintainer-create" id="bounties"><h2>Create a bounty</h2><form id="create-bounty-form" class="maintainer-bounty-form"><label class="field-title">Title<input class="form-input" name="title" maxlength="140" required></label><label class="field-issue">Issue link<input class="form-input" name="issue" type="url" placeholder="https://github.com/owner/repo/issues/123" required></label><label class="field-description">Description<textarea class="form-input" name="description" rows="3" maxlength="4000" required placeholder="Describe the work and what a successful contribution should include."></textarea></label><label>Difficulty<select class="form-input" name="difficulty"><option value="easy">Easy</option><option value="trivial">Trivial</option><option value="medium">Medium</option><option value="hard">Hard</option></select></label><label>Reward (sats)<input class="form-input" name="reward_sats" type="number" min="500" required></label><label>Expires<input class="form-input" name="expires_at" type="date"></label><button class="btn btn-dark" type="submit">Publish bounty</button><p class="dashboard-message" id="create-message" role="status"></p></form></section><section class="maintainer-list" id="your-bounties"><h2>Your bounties</h2><div class="maintainer-table-wrap"><table class="maintainer-table"><thead><tr><th>Title</th><th>Status</th><th>Reward</th><th>Contributor</th></tr></thead><tbody id="project-bounties"><tr><td colspan="4">Loading project bounties…</td></tr></tbody></table></div></section></section></main>`
  const available = document.querySelector<HTMLElement>('#available-balance')!
  const reserved = document.querySelector<HTMLElement>('#reserved-balance')!
  const signIn = () => { app.innerHTML = `${header}<main class="container market-empty"><h1>Sign in to manage this project</h1><p>Your project wallet is only visible to its maintainer.</p><a class="btn btn-dark" href="/api/v1/auth/github">Continue with GitHub ↗</a></main>` }
  if (preview) {
    available.textContent = '[0 sats]'; reserved.textContent = '[0 sats]'
    document.querySelector<HTMLTableSectionElement>('#project-bounties')!.innerHTML = '<tr><td>[Bounty title]</td><td>Open</td><td>[sats]</td><td>None</td></tr><tr><td>[Bounty title]</td><td>In review</td><td>[sats]</td><td>[@handle]</td></tr><tr><td>[Bounty title]</td><td>Paid</td><td>[sats]</td><td>[@handle]</td></tr>'
    document.querySelector<HTMLFormElement>('#fund-form')!.addEventListener('submit', (event) => { event.preventDefault(); document.querySelector('#fund-message')!.textContent = 'Preview only. Register a project to use wallet funding.' })
    document.querySelector<HTMLFormElement>('#create-bounty-form')!.addEventListener('submit', (event) => { event.preventDefault(); document.querySelector('#create-message')!.textContent = 'Preview only. Register a project to publish a bounty.' })
    return
  }
  try {
    const response = await fetch(`/api/v1/projects/${encodeURIComponent(projectId)}/wallet`)
    if (response.status === 401) { signIn(); return }
    if (!response.ok) throw new Error('Could not load this project wallet. Check the project ID and your access.')
    const wallet = await response.json() as { available_sats: number; reserved_sats: number }
    available.textContent = `[${formatSats(wallet.available_sats)} sats]`; reserved.textContent = `[${formatSats(wallet.reserved_sats)} sats]`
  } catch (error) { available.textContent = reserved.textContent = error instanceof Error ? error.message : 'Wallet unavailable.' }
  try {
    const response = await fetch('/api/v1/bounties?limit=100')
    if (response.ok) {
      const data = await response.json() as { items?: Bounty[] }
      const rows = (data.items ?? []).filter((bounty) => bounty.project_id === Number(projectId))
      const table = document.querySelector<HTMLTableSectionElement>('#project-bounties')!
      table.innerHTML = rows.length ? rows.map((bounty) => `<tr><td>${escapeHtml(bounty.title)}</td><td>${escapeHtml(bounty.state)}</td><td>${formatSats(bounty.reward_sats)} sats</td><td>None</td></tr>`).join('') : '<tr><td colspan="4">No open bounties for this project.</td></tr>'
    }
  } catch { /* Keep the table's loading state if the public bounty service is unavailable. */ }
  document.querySelector<HTMLFormElement>('#fund-form')?.addEventListener('submit', async (event) => {
    event.preventDefault()
    const message = document.querySelector<HTMLParagraphElement>('#fund-message')!; message.textContent = 'Creating invoice…'
    try {
      const amount = Number(window.prompt('Amount to add in sats'))
      if (!Number.isFinite(amount) || amount < 1000) throw new Error('Enter a funding amount of at least 1,000 sats.')
      const response = await fetch(`/api/v1/projects/${encodeURIComponent(projectId)}/deposits`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ amount_sats: amount }) })
      const data = await response.json().catch(() => ({})) as { bolt11?: string; error?: { message?: string } }
      if (response.status === 401) { signIn(); return }
      if (!response.ok) throw new Error(data.error?.message ?? 'Could not create an invoice.')
      if (data.bolt11?.includes('placeholder')) throw new Error('Invoice generation is not connected yet. Wallet funding will be available when the Lightning backend is enabled.')
      if (data.bolt11) {
        const invoicePanel = document.querySelector<HTMLDivElement>('#qr-invoice')!
        invoicePanel.classList.add('has-invoice')
        invoicePanel.innerHTML = `<strong>Lightning invoice ready</strong><code>${escapeHtml(data.bolt11)}</code><button type="button" class="copy-invoice">Copy invoice</button>`
        invoicePanel.querySelector<HTMLButtonElement>('.copy-invoice')!.addEventListener('click', async () => {
          try { await navigator.clipboard.writeText(data.bolt11!); message.textContent = 'Invoice copied to clipboard.'; message.className = 'dashboard-message is-success' }
          catch { message.textContent = 'Copy unavailable. Select the invoice text to copy it.'; message.className = 'dashboard-message' }
        })
        message.textContent = 'Pay this invoice with a Lightning wallet. The balance updates after settlement.'
      } else message.textContent = 'Invoice created. Check your Lightning wallet.'
      message.className = 'dashboard-message is-success'
    } catch (error) { message.textContent = error instanceof Error ? error.message : 'Could not create an invoice.'; message.className = 'dashboard-message is-error' }
  })
  document.querySelector<HTMLFormElement>('#create-bounty-form')?.addEventListener('submit', async (event) => {
    event.preventDefault()
    const fields = new FormData(event.currentTarget as HTMLFormElement)
    const message = document.querySelector<HTMLParagraphElement>('#create-message')!; message.textContent = 'Creating bounty…'
    try {
      const issueMatch = String(fields.get('issue')).match(/\/issues\/(\d+)\/?(?:$|[?#])/)
      if (!issueMatch) throw new Error('Enter a GitHub issue URL ending in /issues/<number>.')
      const expires = String(fields.get('expires_at') ?? '')
      const response = await fetch(`/api/v1/projects/${encodeURIComponent(projectId)}/bounties`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ github_issue_number: Number(issueMatch[1]), title: fields.get('title'), description: fields.get('description'), difficulty: fields.get('difficulty'), reward_sats: Number(fields.get('reward_sats')), ...(expires ? { expires_at: `${expires}T23:59:59Z` } : {}) }) })
      const data = await response.json().catch(() => ({})) as Bounty & { error?: { message?: string } }
      if (response.status === 401) { signIn(); return }
      if (!response.ok) throw new Error(data.error?.message ?? 'Could not create bounty. Check your wallet balance and details.')
      message.textContent = `Bounty #${data.id} published and ${formatSats(data.reward_sats)} sats reserved.`; message.className = 'dashboard-message is-success'; (event.currentTarget as HTMLFormElement).reset(); window.setTimeout(() => window.location.reload(), 900)
    } catch (error) { message.textContent = error instanceof Error ? error.message : 'Could not create bounty.'; message.className = 'dashboard-message is-error' }
  })
}
