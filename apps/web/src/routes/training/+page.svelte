<script lang="ts">
  import Glyph from '$components/ui/Glyph.svelte';

  const permissions = [
    {
      permission: 'Create Learning Project',
      workaround: 'Platform Admin team will need to create a Learning Project for each user.'
    },
    {
      permission: 'Edit Learning Project',
      workaround:
        'Platform Admin team will need to create a Learning Course Folder inside the Learning Project and give editor access.'
    },
    {
      permission: 'Upload Files',
      workaround: 'Platform Admin will need to upload files and provide instructions to copy them to the Learning Course Folder.'
    },
    {
      permission: 'Create Ontology assets (Object Types, Links and Actions)',
      workaround: 'None. Permissions are required.'
    }
  ];

  const helpOptions = [
    {
      title: 'Training Application',
      text:
        "Your organization's Foundry Platform Administrator may have posted local documentation within Foundry's Training application. This can cover local restrictions for projects, uploads, ontology editing, and Marketplace deployments."
    },
    {
      title: 'Permissions',
      text:
        'To complete this learning course you need access to Workspace Navigator (Compass), Pipeline Builder, Ontology Manager, Object Explorer, and Workshop.'
    },
    {
      title: 'Documentation & Feedback',
      text:
        'Search product documentation at palantir.com/docs/foundry and report screenshot or workflow issues to training-feedback@palantir.com.'
    }
  ];
</script>

<svelte:head>
  <title>OpenFoundry — Training</title>
</svelte:head>

<div class="training-shell">
  <section class="training-hero of-panel">
    <div class="training-hero__header">
      <div>
        <div class="of-eyebrow">Foundry &amp; AIP Builder Foundations</div>
        <h1 class="training-hero__title">Build a fully-functional end-to-end workflow in 60 minutes</h1>
      </div>
      <div class="training-hero__meta">
        <span class="training-pill">Continuously updated</span>
        <span class="training-pill">Course + operational walkthrough</span>
      </div>
    </div>

    <div class="training-copy">
      <p>
        This course is continuously updated. If you have feedback, please email
        <a href="mailto:training-feedback@palantir.com">training-feedback@palantir.com</a>.
      </p>
      <p>
        In the next 60 minutes you will build a fully-functional end-to-end workflow. The goal is to give you a practical feel for major Foundry components so the deeper trainings make immediate sense.
      </p>
      <p>
        You can view this course material without logging in to Palantir Learn, but you will need an account to take the quiz, earn the Foundry &amp; AIP Builder Foundations badge, or leave course feedback.
      </p>
      <p>
        Ontologize also provides a step-by-step YouTube walkthrough if you prefer to follow the course in video format while completing the material.
      </p>
    </div>
  </section>

  <section class="training-grid">
    <article class="training-section of-panel">
      <div class="training-section__header">
        <div class="of-eyebrow">Permissions &amp; Help</div>
        <h2 class="of-heading-lg">What you need before starting</h2>
      </div>

      <div class="help-grid">
        {#each helpOptions as option}
          <div class="help-card">
            <h3>{option.title}</h3>
            <p>{option.text}</p>
          </div>
        {/each}
      </div>

      <div class="training-note">
        Screenshots may differ slightly from your Foundry instance because the platform evolves quickly. Most differences should remain cosmetic, but if they block the exercise, report them through the feedback channel.
      </div>

      <div class="table-wrap">
        <table class="of-table">
          <thead>
            <tr>
              <th>Permissions Required</th>
              <th>Workaround if permissions are not available</th>
            </tr>
          </thead>
          <tbody>
            {#each permissions as row}
              <tr>
                <td>{row.permission}</td>
                <td>{row.workaround}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </article>

    <article class="training-section of-panel">
      <div class="training-section__header">
        <div class="of-eyebrow">The Use Case</div>
        <h2 class="of-heading-lg">Office Goods Corp needs one operational view</h2>
      </div>

      <div class="training-copy">
        <p>
          Office Goods Corp, a paper and office goods company, recently acquired Bureau SAS. The IT unification project will not start for another year, so the fulfillment team is still working across two separate order-management systems.
        </p>
        <p>
          Teams have been stitching spreadsheets together by hand, but growth is creating incorrect metrics, missed fulfillment, and unhappy customers. The objective of this workflow is to give managers and operators a single place to assign orders, track unassigned work, and spot deliveries at risk.
        </p>
      </div>

      <div class="use-case-list">
        <div class="use-case-list__item">
          <span class="use-case-list__bullet"></span>
          <div>
            <strong>Managers</strong>
            <p>Track performance and identify orders at risk of missing delivery commitments.</p>
          </div>
        </div>
        <div class="use-case-list__item">
          <span class="use-case-list__bullet"></span>
          <div>
            <strong>Operators</strong>
            <p>Assign open orders from both historical fulfillment systems without debating which spreadsheet is current.</p>
          </div>
        </div>
        <div class="use-case-list__item">
          <span class="use-case-list__bullet"></span>
          <div>
            <strong>Platform workflow</strong>
            <p>Connect data sources, model the ontology, and publish a simple operational application for fulfillment teams.</p>
          </div>
        </div>
      </div>
    </article>
  </section>

  <section class="training-section of-panel">
    <div class="training-section__header">
      <div class="of-eyebrow">Context Diagram</div>
      <h2 class="of-heading-lg">Office Goods Corp operating model</h2>
    </div>

    <div class="architecture-surface">
      <div class="architecture-title">Office Goods Corp</div>

      <div class="diagram-card diagram-card--managers">
        <div class="diagram-card__rail">
          <span class="diagram-card__icon diagram-card__icon--user" aria-hidden="true">
            <svg viewBox="0 0 24 24">
              <circle cx="12" cy="12" r="10"></circle>
              <path d="M12 7.3a3.1 3.1 0 1 1 0 6.2a3.1 3.1 0 0 1 0-6.2Zm-5.3 12a5.6 5.6 0 0 1 10.6 0"></path>
            </svg>
          </span>
          <span class="diagram-card__type">User Group</span>
        </div>
        <div class="diagram-card__content">
          <h3>Fulfillment Managers</h3>
          <p>People that need to track performance and orders at risk of not being delivered.</p>
        </div>
      </div>

      <div class="diagram-card diagram-card--service-left">
        <div class="diagram-card__rail">
          <span class="diagram-card__icon diagram-card__icon--service" aria-hidden="true">
            <Glyph name="database" size={34} strokeWidth={1.6} />
          </span>
          <span class="diagram-card__type">Service</span>
        </div>
        <div class="diagram-card__content">
          <h3>Office Goods Fulfillment</h3>
          <p>Historical Fulfillment System for Office Goods Corp.</p>
        </div>
      </div>

      <div class="diagram-card diagram-card--service-right">
        <div class="diagram-card__rail">
          <span class="diagram-card__icon diagram-card__icon--service" aria-hidden="true">
            <Glyph name="database" size={34} strokeWidth={1.6} />
          </span>
          <span class="diagram-card__type">Service</span>
        </div>
        <div class="diagram-card__content">
          <h3>Bureau Fulfillment</h3>
          <p>Historical Fulfillment System for Bureau SAS before integration into Office Goods.</p>
        </div>
      </div>

      <div class="diagram-card diagram-card--operators">
        <div class="diagram-card__rail">
          <span class="diagram-card__icon diagram-card__icon--user" aria-hidden="true">
            <svg viewBox="0 0 24 24">
              <circle cx="12" cy="12" r="10"></circle>
              <path d="M12 7.3a3.1 3.1 0 1 1 0 6.2a3.1 3.1 0 0 1 0-6.2Zm-5.3 12a5.6 5.6 0 0 1 10.6 0"></path>
            </svg>
          </span>
          <span class="diagram-card__type">User Group</span>
        </div>
        <div class="diagram-card__content">
          <h3>Fulfillment Operators</h3>
          <p>People in charge of assigning open orders.</p>
        </div>
      </div>

      <svg class="architecture-lines" viewBox="0 0 1000 760" preserveAspectRatio="none" aria-hidden="true">
        <defs>
          <marker id="training-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M0 0 10 5 0 10Z" fill="#111827"></path>
          </marker>
        </defs>
        <path
          d="M290 436 C290 492 360 500 404 548 C434 580 452 620 452 661"
          marker-start="url(#training-arrow)"
        ></path>
        <path
          d="M710 436 C710 492 640 500 596 548 C566 580 548 620 548 661"
          marker-start="url(#training-arrow)"
        ></path>
      </svg>
    </div>
  </section>

  <section class="training-grid training-grid--bottom">
    <article class="training-section of-panel">
      <div class="training-section__header">
        <div class="of-eyebrow">Workflow Outcome</div>
        <h2 class="of-heading-lg">What this learning project should produce</h2>
      </div>
      <div class="training-copy">
        <p>Create one Learning Project where the connected systems, ontology assets, and Workshop experience live together for the duration of the course.</p>
        <p>Use a naming convention that clearly separates your learning assets from those created by other users in the environment.</p>
      </div>
    </article>

    <article class="training-section of-panel">
      <div class="training-section__header">
        <div class="of-eyebrow">Reference</div>
        <h2 class="of-heading-lg">Helpful links</h2>
      </div>
      <div class="link-list">
        <a class="link-list__item" href="https://www.palantir.com/docs/foundry" target="_blank" rel="noreferrer">
          <span>Foundry documentation</span>
          <span aria-hidden="true">↗</span>
        </a>
        <a class="link-list__item" href="mailto:training-feedback@palantir.com">
          <span>training-feedback@palantir.com</span>
          <span aria-hidden="true">✉</span>
        </a>
      </div>
    </article>
  </section>
</div>

<style>
  .training-shell {
    display: grid;
    gap: 18px;
    max-width: 1440px;
    margin: 0 auto;
  }

  .training-grid {
    display: grid;
    gap: 18px;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .training-grid--bottom {
    grid-template-columns: minmax(0, 1.35fr) minmax(320px, 0.65fr);
  }

  .training-hero,
  .training-section {
    padding: 22px 24px;
    background: linear-gradient(180deg, #fffefd 0%, #fbf8f3 100%);
  }

  .training-hero__header,
  .training-section__header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 18px;
  }

  .training-hero__title {
    margin: 6px 0 0;
    color: var(--text-strong);
    font-size: 30px;
    line-height: 1.15;
    letter-spacing: -0.03em;
  }

  .training-hero__meta {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }

  .training-pill {
    display: inline-flex;
    align-items: center;
    min-height: 28px;
    padding: 0 12px;
    border: 1px solid #e5ddd2;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.86);
    color: var(--text-default);
    font-size: 12px;
    font-weight: 600;
  }

  .training-copy {
    display: grid;
    gap: 12px;
    color: var(--text-default);
    font-size: 15px;
    line-height: 1.72;
  }

  .training-copy p {
    margin: 0;
  }

  .help-grid {
    display: grid;
    gap: 14px;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    margin-bottom: 16px;
  }

  .help-card {
    padding: 16px;
    border: 1px solid #e7e1d8;
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.92);
  }

  .help-card h3,
  .diagram-card__content h3 {
    margin: 0;
    color: var(--text-strong);
    font-size: 16px;
    font-weight: 700;
  }

  .help-card p,
  .diagram-card__content p {
    margin: 10px 0 0;
    color: var(--text-default);
    font-size: 14px;
    line-height: 1.6;
  }

  .training-note {
    margin-bottom: 16px;
    padding: 12px 14px;
    border: 1px solid #edd8b5;
    border-radius: 8px;
    background: #fff7ea;
    color: #8a5b09;
    line-height: 1.6;
  }

  .table-wrap {
    overflow-x: auto;
  }

  .use-case-list {
    display: grid;
    gap: 14px;
    margin-top: 16px;
  }

  .use-case-list__item {
    display: grid;
    grid-template-columns: 12px minmax(0, 1fr);
    gap: 12px;
    align-items: start;
  }

  .use-case-list__bullet {
    width: 10px;
    height: 10px;
    margin-top: 7px;
    border-radius: 999px;
    background: #2f8f5b;
    box-shadow: 0 0 0 4px rgba(47, 143, 91, 0.12);
  }

  .use-case-list__item strong {
    display: block;
    margin-bottom: 4px;
    color: var(--text-strong);
  }

  .use-case-list__item p {
    margin: 0;
    color: var(--text-default);
    line-height: 1.6;
  }

  .architecture-surface {
    position: relative;
    min-height: 980px;
    padding: 44px 28px 28px;
    border: 1px solid #d7d9de;
    border-radius: 6px;
    background: #fff;
    overflow: hidden;
  }

  .architecture-title {
    position: absolute;
    top: 40px;
    left: 50%;
    transform: translateX(-50%);
    color: #1f2937;
    font-size: 26px;
    font-weight: 700;
    letter-spacing: -0.02em;
  }

  .diagram-card {
    position: absolute;
    display: grid;
    grid-template-columns: 170px minmax(0, 1fr);
    width: 380px;
    min-height: 190px;
    border: 1px solid #d4d7dd;
    border-radius: 6px;
    background: #fff;
    box-shadow: 0 1px 2px rgba(28, 39, 60, 0.04);
  }

  .diagram-card--managers {
    top: 138px;
    left: 50%;
    transform: translateX(-50%);
    width: 500px;
    min-height: 210px;
  }

  .diagram-card--service-left {
    top: 450px;
    left: 86px;
  }

  .diagram-card--service-right {
    top: 450px;
    right: 86px;
  }

  .diagram-card--operators {
    bottom: 84px;
    left: 50%;
    transform: translateX(-50%);
    width: 470px;
    min-height: 210px;
  }

  .diagram-card__rail {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 14px;
    padding: 24px 18px;
    border-right: 1px solid #d8dbe0;
  }

  .diagram-card__type {
    color: #1f2937;
    font-size: 15px;
    font-weight: 700;
    text-align: center;
  }

  .diagram-card__content {
    padding: 32px 34px 28px;
  }

  .diagram-card__icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 52px;
    height: 52px;
  }

  .diagram-card__icon--user {
    color: #2f8f5b;
  }

  .diagram-card__icon--user svg {
    width: 52px;
    height: 52px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.7;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .diagram-card__icon--service {
    color: #94a3b8;
  }

  .architecture-lines {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }

  .architecture-lines path {
    fill: none;
    stroke: #f4a307;
    stroke-width: 2.2;
    stroke-dasharray: 8 11;
    stroke-linecap: round;
  }

  .link-list {
    display: grid;
    gap: 12px;
  }

  .link-list__item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 16px;
    border: 1px solid #e1e5ec;
    border-radius: 8px;
    background: #fff;
    color: var(--text-strong);
    font-weight: 600;
  }

  .link-list__item:hover {
    text-decoration: none;
    border-color: #c8d2e3;
    background: #f8fbff;
  }

  @media (max-width: 1280px) {
    .training-grid,
    .training-grid--bottom,
    .help-grid {
      grid-template-columns: 1fr;
    }

    .architecture-surface {
      min-height: auto;
      display: grid;
      gap: 18px;
      padding-top: 90px;
    }

    .diagram-card,
    .diagram-card--managers,
    .diagram-card--service-left,
    .diagram-card--service-right,
    .diagram-card--operators {
      position: relative;
      top: auto;
      right: auto;
      bottom: auto;
      left: auto;
      width: 100%;
      min-height: 0;
      transform: none;
    }

    .architecture-lines {
      display: none;
    }
  }

  @media (max-width: 720px) {
    .training-hero,
    .training-section {
      padding: 18px;
    }

    .training-hero__header,
    .training-section__header {
      flex-direction: column;
    }

    .training-hero__title {
      font-size: 25px;
    }

    .diagram-card {
      grid-template-columns: 1fr;
    }

    .diagram-card__rail {
      border-right: 0;
      border-bottom: 1px solid #d8dbe0;
    }

    .diagram-card__content {
      padding: 24px 20px;
    }
  }
</style>
