import './styles/main.css'
import 'htmx.org'

type User = { github_login: string }

const app = document.querySelector<HTMLDivElement>('#app')

if (!app) throw new Error('App root is missing')

app.innerHTML = `
  <header class="site-header">
    <div class="container nav">
      <a class="nav-brand" href="#top" aria-label="Satlight home"><span class="brand-mark">₿</span> satlight</a>
      <nav class="nav-links" aria-label="Main navigation">
        <a class="nav-link" href="#how-it-works">How it works</a>
        <a class="nav-link" href="#for-projects">For projects</a>
        <a class="btn btn-dark nav-cta" href="#join">Join the waitlist <span aria-hidden="true">↗</span></a>
      </nav>
    </div>
  </header>

  <main id="top">
    <section class="hero">
      <div class="container hero-grid">
        <div class="hero-copy">
          <p class="eyebrow"><span class="eyebrow-dot"></span> OPEN SOURCE, REWARDED</p>
          <h1>Good work deserves<br><span>good sats.</span></h1>
          <p class="hero-lede">Satlight helps open-source maintainers fund the small, important work that keeps projects moving. Contributors earn Bitcoin for work that gets merged.</p>
          <div class="hero-actions">
            <a class="btn btn-primary btn-large" href="#join">Get early access <span aria-hidden="true">→</span></a>
            <a class="text-link" href="#how-it-works">See how it works <span aria-hidden="true">↓</span></a>
          </div>
          <p class="hero-note"><span aria-hidden="true">⚡</span> Built for open source. Powered by Lightning.</p>
        </div>
        <div class="hero-art" aria-label="A preview of a funded open-source task">
          <div class="orbit orbit-one"></div><div class="orbit orbit-two"></div>
          <div class="spark spark-a">✳</div><div class="spark spark-b">✦</div>
          <article class="bounty-card">
            <div class="bounty-card-top"><span class="bounty-badge"><span></span> OPEN BOUNTY</span><span class="dots">•••</span></div>
            <p class="bounty-repo">◈ &nbsp; northstar / docs</p>
            <h2>Add a getting started guide</h2>
            <p class="bounty-desc">Help new contributors make their first commit with a clear setup guide.</p>
            <div class="bounty-tags"><span>Documentation</span><span>Easy</span></div>
            <div class="bounty-bottom"><div class="reward"><span>REWARD</span><strong>15,000 <small>sats</small></strong></div><span class="lightning-pill">⚡ Lightning</span></div>
          </article>
          <div class="paid-note"><span class="paid-icon">✓</span><span><strong>PR merged</strong><small>15,000 sats paid</small></span><span class="paid-flash">⚡</span></div>
          <div class="hero-caption">Small fixes. Real rewards.</div>
        </div>
      </div>
      <div class="hero-gridline" aria-hidden="true"></div>
    </section>

    <section class="trust-strip"><div class="container trust-inner"><span>MADE FOR THE OPEN SOURCE COMMUNITY</span><i></i><span>NO BANKS OR PAYMENT PROCESSORS</span><i></i><span>INSTANT PAYOUTS OVER LIGHTNING</span></div></section>

    <section class="section how-section" id="how-it-works">
      <div class="container">
        <div class="section-heading"><p class="eyebrow">A BETTER WAY TO CONTRIBUTE</p><h2>From issue to sats.<br><span>Simple as that.</span></h2><p>Keep your workflow on GitHub. Satlight takes care of the reward.</p></div>
        <div class="steps-grid">
          <article class="step-card"><span class="step-number">01</span><div class="step-icon icon-fund">↗</div><h3>Fund a project</h3><p>Maintainers add sats to a project wallet and put rewards on work their community needs.</p><span class="step-line"></span></article>
          <article class="step-card"><span class="step-number">02</span><div class="step-icon icon-build">⌘</div><h3>Make a contribution</h3><p>Pick a task, make your change, and open a pull request the way you already do.</p><span class="step-line"></span></article>
          <article class="step-card"><span class="step-number">03</span><div class="step-icon icon-earn">⚡</div><h3>Get paid on merge</h3><p>When your pull request is merged, your reward is sent over the Lightning Network.</p></article>
        </div>
      </div>
    </section>

    <section class="project-section" id="for-projects"><div class="container project-grid">
      <div class="project-copy"><p class="eyebrow">FOR MAINTAINERS</p><h2>Make room for<br>the work that <span>matters.</span></h2><p>Turn your good-first-issue backlog into real opportunities. Fund tasks with Bitcoin and invite contributors everywhere to help.</p><ul class="check-list"><li><span>✓</span> Keep issues and pull requests on GitHub</li><li><span>✓</span> Set clear rewards for useful work</li><li><span>✓</span> Reach contributors around the world</li></ul><a class="btn btn-dark btn-large" href="#register">Register a project <span aria-hidden="true">→</span></a></div>
      <div class="project-visual"><div class="repo-window"><div class="window-top"><span></span><span></span><span></span><b>northstar / project</b></div><div class="repo-title"><span class="repo-avatar">N</span><div><strong>northstar</strong><small>Open source, moving forward.</small></div><span class="repo-public">PUBLIC</span></div><div class="repo-stats"><span>◉ &nbsp; 2.4k stars</span><span>⑂ &nbsp; 318 forks</span></div><div class="issue-row"><span class="issue-open">●</span><div><strong>Improve install docs for Linux</strong><small>#284 opened 2 days ago</small></div><span class="issue-reward">10k sats</span></div><div class="issue-row"><span class="issue-open">●</span><div><strong>Add Spanish translations</strong><small>#279 opened 5 days ago</small></div><span class="issue-reward">25k sats</span></div><div class="issue-row"><span class="issue-open">●</span><div><strong>Fix mobile navigation spacing</strong><small>#275 opened 1 week ago</small></div><span class="issue-reward">8k sats</span></div><div class="repo-foot"><span>✦ &nbsp; 3 tasks with rewards</span><span class="repo-foot-bolt">⚡</span></div></div><div class="floating-sat">₿<small> sats</small></div></div>
    </div></section>

    <section class="join-section" id="join"><div class="container join-grid">
      <div class="join-copy"><p class="eyebrow">BE THERE FROM THE START</p><h2>Help shape<br><span>what comes next.</span></h2><p>We’re inviting maintainers and contributors to join the early community. Sign up for updates and be first to try Satlight.</p><div class="join-proof"><div class="avatar-stack"><span>M</span><span>J</span><span>A</span><span>+</span></div><span>Built with the community, for the community.</span></div></div>
      <div class="signup-card"><div class="signup-card-head"><span class="signup-mark">✳</span><div><h3>Get early access</h3><p>Tell us where you fit in.</p></div></div><form id="waitlist-form"><label class="form-label" for="waitlist-email">Email address</label><input class="form-input" id="waitlist-email" name="email" type="email" autocomplete="email" placeholder="you@example.com" required><fieldset class="role-options"><legend>I’m joining as a...</legend><label><input type="radio" name="role" value="maintainer" checked><span>Maintainer</span></label><label><input type="radio" name="role" value="contributor"><span>Contributor</span></label></fieldset><button class="btn btn-primary btn-large signup-submit" type="submit">Join the waitlist <span aria-hidden="true">→</span></button><p class="form-message" id="waitlist-message" role="status" aria-live="polite"></p><p class="privacy-note">No spam. Just occasional progress and launch updates.</p></form></div>
    </div></section>

    <section class="register-section" id="register"><div class="container register-inner"><div><p class="eyebrow">PROJECT REGISTRATION</p><h2>Bring your repository<br>to <span>Satlight.</span></h2><p>Sign in with GitHub, install the Satlight GitHub App on your repository, then register it here.</p></div><div class="register-card" id="register-card"><p class="register-loading">Checking your GitHub session…</p></div></div></section>
  </main>

  <footer class="site-footer"><div class="container footer-inner"><a class="nav-brand" href="#top"><span class="brand-mark">₿</span> satlight</a><span>Rewarding the work that moves open source forward.</span><a href="#top">Back to top ↑</a></div><div class="container footer-bottom"><span>© 2026 Satlight. Early development.</span><span>Bitcoin payments involve risk. Use test funds during early access.</span></div></footer>
`

const waitlistForm = document.querySelector<HTMLFormElement>('#waitlist-form')!
const waitlistMessage = document.querySelector<HTMLParagraphElement>('#waitlist-message')!

waitlistForm.addEventListener('submit', async (event) => {
  event.preventDefault()
  const submit = waitlistForm.querySelector<HTMLButtonElement>('button[type="submit"]')!
  const data = new FormData(waitlistForm)
  const email = String(data.get('email') ?? '').trim()
  const role = String(data.get('role') ?? 'contributor')
  submit.disabled = true
  submit.textContent = 'Joining…'
  waitlistMessage.textContent = ''
  waitlistMessage.className = 'form-message'

  try {
    const response = await fetch('/api/v1/waitlist', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, role }),
    })
    if (!response.ok) throw new Error(response.status === 429 ? 'Too many attempts. Please try again shortly.' : 'We could not add you right now. Please try again.')
    waitlistMessage.textContent = 'You’re on the list. We’ll be in touch when early access opens.'
    waitlistMessage.classList.add('is-success')
    waitlistForm.reset()
  } catch (error) {
    waitlistMessage.textContent = error instanceof Error ? error.message : 'Something went wrong. Please try again.'
    waitlistMessage.classList.add('is-error')
  } finally {
    submit.disabled = false
    submit.innerHTML = 'Join the waitlist <span aria-hidden="true">→</span>'
  }
})

const registerCard = document.querySelector<HTMLDivElement>('#register-card')!

function showSignIn(message = 'Sign in with GitHub to register a repository you maintain.') {
  registerCard.innerHTML = `<p>${message}</p><a class="btn btn-dark register-button" href="/api/v1/auth/github">Continue with GitHub <span aria-hidden="true">↗</span></a><small>Satlight will verify your repository access before registration.</small>`
}

function showProjectForm(user: User) {
  registerCard.innerHTML = `<p class="signed-in-as">Signed in as <strong>@${escapeHtml(user.github_login)}</strong></p><form id="project-form"><label class="form-label" for="repo-name">GitHub repository</label><input class="form-input" id="repo-name" name="repo_full_name" placeholder="owner/repository" pattern="[^/\\s]+/[^/\\s]+" required><label class="form-label" for="installation-id">GitHub App installation ID</label><input class="form-input" id="installation-id" name="installation_id" type="number" min="1" step="1" placeholder="Shown in your GitHub App installation URL" required><p class="register-hint">Install the Satlight GitHub App on the repository first. Its installation ID appears in the URL after setup.</p><button class="btn btn-primary register-button" type="submit">Register repository <span aria-hidden="true">→</span></button><p class="register-message" id="project-message" role="status" aria-live="polite"></p></form>`

  const projectForm = registerCard.querySelector<HTMLFormElement>('#project-form')!
  const message = registerCard.querySelector<HTMLParagraphElement>('#project-message')!
  projectForm.addEventListener('submit', async (event) => {
    event.preventDefault()
    const button = projectForm.querySelector<HTMLButtonElement>('button')!
    const fields = new FormData(projectForm)
    button.disabled = true
    message.textContent = 'Checking repository access…'
    try {
      const response = await fetch('/api/v1/projects', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo_full_name: String(fields.get('repo_full_name')).trim(), installation_id: Number(fields.get('installation_id')) }),
      })
      const result = await response.json().catch(() => ({})) as { repo_full_name?: string; error?: { message?: string } }
      if (response.status === 401) {
        showSignIn('Your session expired. Sign in again to register the repository.')
        return
      }
      if (!response.ok) throw new Error(result.error?.message ?? 'We could not register that repository. Check the name and installation ID.')
      message.textContent = `${result.repo_full_name ?? 'Repository'} is registered. You’re ready for the next step.`
      message.className = 'register-message is-success'
      projectForm.reset()
    } catch (error) {
      message.textContent = error instanceof Error ? error.message : 'Registration failed. Please try again.'
      message.className = 'register-message is-error'
    } finally {
      button.disabled = false
    }
  })
}

function escapeHtml(value: string) {
  const element = document.createElement('span')
  element.textContent = value
  return element.innerHTML
}

fetch('/api/v1/me').then(async (response) => {
  if (!response.ok) {
    showSignIn()
    return
  }
  showProjectForm(await response.json() as User)
}).catch(() => showSignIn('The registration service is unavailable right now. You can still join the waitlist above.'))
