import { useMemo, useState } from 'react';

import {
  acknowledgeHealthAlert,
  buildHealthEmailDigest,
  createHealthAlertSubscription,
  deleteHealthAlertSubscription,
  dispatchHealthAlerts,
  generateHealthReportSnapshot,
  healthReportStatusLabel,
  latestReportsForResource,
  latestReportsForResources,
  listHealthAlertSubscriptions,
  listHealthAlerts,
  listHealthReportHistory,
  recordHealthReportSnapshot,
  rollupHealthReportStatus,
  type HealthAlertChannel,
  type HealthAlertSubscription,
  type HealthCheckReport,
  type HealthDigestFrequency,
  type HealthReportSignal,
  type HealthReportSnapshot,
  type HealthReportStatus,
} from '@/lib/api/health-reports';
import { resourceHealthCheckLabel } from '@/lib/api/resource-health-checks';
import { notifications } from '@/lib/stores/notifications';

interface HealthReportsPanelProps {
  signals: HealthReportSignal[];
}

interface ResourceHealthStatusBadgeProps {
  resourceRid?: string | null;
  compact?: boolean;
}

interface ProjectHealthSummaryProps {
  resourceRids: string[];
  title?: string;
}

const CHANNELS: Array<{ value: HealthAlertChannel; label: string }> = [
  { value: 'in_app', label: 'In-platform' },
  { value: 'email_digest', label: 'Email digest' },
  { value: 'slack', label: 'Slack' },
  { value: 'pagerduty', label: 'PagerDuty' },
  { value: 'webhook', label: 'Webhook' },
];

const DIGEST_FREQUENCIES: HealthDigestFrequency[] = ['immediate', 'hourly', 'daily', 'weekly'];
const MINIMUM_STATUSES: Exclude<HealthReportStatus, 'passing'>[] = ['warning', 'failing', 'unknown'];

export function HealthReportsPanel({ signals }: HealthReportsPanelProps) {
  const [history, setHistory] = useState<HealthReportSnapshot[]>(() => listHealthReportHistory());
  const [alerts, setAlerts] = useState(() => listHealthAlerts());
  const [subscriptions, setSubscriptions] = useState<HealthAlertSubscription[]>(() => listHealthAlertSubscriptions());
  const [name, setName] = useState('Production health alerts');
  const [scopeResourceRid, setScopeResourceRid] = useState('');
  const [scopeMonitoringView, setScopeMonitoringView] = useState('');
  const [minimumStatus, setMinimumStatus] = useState<Exclude<HealthReportStatus, 'passing'>>('warning');
  const [channels, setChannels] = useState<HealthAlertChannel[]>(['in_app', 'email_digest']);
  const [digestFrequency, setDigestFrequency] = useState<HealthDigestFrequency>('daily');
  const [emailRecipients, setEmailRecipients] = useState('');
  const [slackDestinationRef, setSlackDestinationRef] = useState('');
  const [pagerDutyServiceRef, setPagerDutyServiceRef] = useState('');
  const [webhookDestinationRef, setWebhookDestinationRef] = useState('');

  const latest = history[0] ?? null;
  const unacknowledgedAlerts = alerts.filter((alert) => !alert.acknowledged_at);

  function reloadStores() {
    setHistory(listHealthReportHistory());
    setAlerts(listHealthAlerts());
    setSubscriptions(listHealthAlertSubscriptions());
  }

  function generateReport() {
    const snapshot = recordHealthReportSnapshot(generateHealthReportSnapshot(signals));
    const createdAlerts = dispatchHealthAlerts(snapshot);
    reloadStores();
    if (createdAlerts.length > 0) {
      notifications.warning(`${createdAlerts.length} Data Health alert(s) created`);
    } else {
      notifications.success('Data Health report generated');
    }
  }

  async function copyDigest() {
    if (!latest) return;
    const digest = buildHealthEmailDigest(latest);
    try {
      await navigator.clipboard.writeText(digest);
      notifications.success('Email digest copied');
    } catch {
      notifications.info(digest);
    }
  }

  function toggleChannel(channel: HealthAlertChannel) {
    setChannels((current) => (
      current.includes(channel)
        ? current.filter((value) => value !== channel)
        : [...current, channel]
    ));
  }

  function addSubscription() {
    const selectedChannels: HealthAlertChannel[] = channels.length > 0 ? channels : ['in_app'];
    createHealthAlertSubscription({
      name: name.trim() || 'Data Health subscription',
      scope_resource_rid: scopeResourceRid.trim(),
      scope_monitoring_view: scopeMonitoringView.trim(),
      minimum_status: minimumStatus,
      channels: selectedChannels,
      email_recipients: emailRecipients.trim(),
      digest_frequency: digestFrequency,
      slack_destination_ref: slackDestinationRef.trim(),
      pagerduty_service_ref: pagerDutyServiceRef.trim(),
      webhook_destination_ref: webhookDestinationRef.trim(),
    });
    setName('Production health alerts');
    setScopeResourceRid('');
    setScopeMonitoringView('');
    reloadStores();
    notifications.success('Health alert subscription saved');
  }

  function removeSubscription(id: string) {
    deleteHealthAlertSubscription(id);
    reloadStores();
  }

  function acknowledge(id: string) {
    acknowledgeHealthAlert(id);
    reloadStores();
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 14 }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'start', flexWrap: 'wrap' }}>
        <div>
          <p className="of-eyebrow">Reports and alerting</p>
          <h2 className="of-heading-md" style={{ marginTop: 4 }}>Health reports</h2>
          <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 13, maxWidth: 780 }}>
            Latest and historical check reports with in-platform alert records, email digest text, and destination references for Slack, PagerDuty, and webhooks.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <button type="button" className="of-button of-button--primary" onClick={generateReport}>
            Generate report
          </button>
          <button type="button" className="of-button" onClick={() => void copyDigest()} disabled={!latest}>
            Copy digest
          </button>
        </div>
      </header>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 10 }}>
        <ReportStat label="Signal inputs" value={signals.length.toLocaleString()} />
        <ReportStat label="Latest reports" value={(latest?.summary.total ?? 0).toLocaleString()} />
        <ReportStat label="Failing" value={(latest?.summary.failing ?? 0).toLocaleString()} status="failing" />
        <ReportStat label="Warning" value={(latest?.summary.warning ?? 0).toLocaleString()} status="warning" />
        <ReportStat label="Subscriptions" value={subscriptions.length.toLocaleString()} />
        <ReportStat label="Open alerts" value={unacknowledgedAlerts.length.toLocaleString()} status={unacknowledgedAlerts.length > 0 ? 'failing' : 'passing'} />
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 360px), 1fr))', gap: 14, alignItems: 'start' }}>
        <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
          <div>
            <p className="of-eyebrow">Latest check reports</p>
            <h3 className="of-heading-sm" style={{ marginTop: 4 }}>
              {latest ? formatDate(latest.generated_at) : 'No report generated'}
            </h3>
          </div>
          {latest ? (
            <div style={{ display: 'grid', gap: 8 }}>
              {latest.reports.slice(0, 10).map((report) => <ReportRow key={report.id} report={report} />)}
            </div>
          ) : (
            <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>
              Generate a report to capture the current scoped monitoring view as history.
            </p>
          )}
        </section>

        <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
          <div>
            <p className="of-eyebrow">Subscriptions</p>
            <h3 className="of-heading-sm" style={{ marginTop: 4 }}>Alert destinations</h3>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: 8 }}>
            <label style={{ fontSize: 12 }}>
              Name
              <input value={name} onChange={(event) => setName(event.target.value)} className="of-input" style={{ marginTop: 4 }} />
            </label>
            <label style={{ fontSize: 12 }}>
              Minimum status
              <select value={minimumStatus} onChange={(event) => setMinimumStatus(event.target.value as Exclude<HealthReportStatus, 'passing'>)} className="of-input" style={{ marginTop: 4 }}>
                {MINIMUM_STATUSES.map((status) => <option key={status} value={status}>{healthReportStatusLabel(status)}</option>)}
              </select>
            </label>
            <label style={{ fontSize: 12 }}>
              Digest frequency
              <select value={digestFrequency} onChange={(event) => setDigestFrequency(event.target.value as HealthDigestFrequency)} className="of-input" style={{ marginTop: 4 }}>
                {DIGEST_FREQUENCIES.map((frequency) => <option key={frequency} value={frequency}>{frequency}</option>)}
              </select>
            </label>
            <label style={{ fontSize: 12 }}>
              Resource RID scope
              <input value={scopeResourceRid} onChange={(event) => setScopeResourceRid(event.target.value)} className="of-input" style={{ marginTop: 4 }} placeholder="optional RID" />
            </label>
            <label style={{ fontSize: 12 }}>
              Monitoring view scope
              <input value={scopeMonitoringView} onChange={(event) => setScopeMonitoringView(event.target.value)} className="of-input" style={{ marginTop: 4 }} placeholder="project or view ref" />
            </label>
            <label style={{ fontSize: 12 }}>
              Email recipients
              <input value={emailRecipients} onChange={(event) => setEmailRecipients(event.target.value)} className="of-input" style={{ marginTop: 4 }} placeholder="team@example.com" />
            </label>
            <label style={{ fontSize: 12 }}>
              Slack destination ref
              <input value={slackDestinationRef} onChange={(event) => setSlackDestinationRef(event.target.value)} className="of-input" style={{ marginTop: 4 }} placeholder="ops-data-health" />
            </label>
            <label style={{ fontSize: 12 }}>
              PagerDuty service ref
              <input value={pagerDutyServiceRef} onChange={(event) => setPagerDutyServiceRef(event.target.value)} className="of-input" style={{ marginTop: 4 }} placeholder="pd-service-data" />
            </label>
            <label style={{ fontSize: 12 }}>
              Webhook destination ref
              <input value={webhookDestinationRef} onChange={(event) => setWebhookDestinationRef(event.target.value)} className="of-input" style={{ marginTop: 4 }} placeholder="webhook:data-health" />
            </label>
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {CHANNELS.map((channel) => (
              <label key={channel.value} className="of-chip" style={{ cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={channels.includes(channel.value)}
                  onChange={() => toggleChannel(channel.value)}
                  style={{ margin: 0 }}
                />
                {channel.label}
              </label>
            ))}
          </div>
          <button type="button" className="of-button" onClick={addSubscription} style={{ justifySelf: 'start' }}>
            Save subscription
          </button>
          {subscriptions.length > 0 && (
            <div style={{ display: 'grid', gap: 8 }}>
              {subscriptions.map((subscription) => (
                <div key={subscription.id} style={{ display: 'grid', gap: 4, borderTop: '1px solid var(--border-subtle)', paddingTop: 8 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
                    <strong style={{ fontSize: 13 }}>{subscription.name}</strong>
                    <button type="button" className="of-button" onClick={() => removeSubscription(subscription.id)}>Delete</button>
                  </div>
                  <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                    {subscription.channels.join(', ')} | {healthReportStatusLabel(subscription.minimum_status)}+ | {subscription.digest_frequency}
                  </p>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
          <div>
            <p className="of-eyebrow">Alert history</p>
            <h3 className="of-heading-sm" style={{ marginTop: 4 }}>{alerts.length.toLocaleString()} alert(s)</h3>
          </div>
          <span className="of-text-muted" style={{ fontSize: 12 }}>
            {history.length.toLocaleString()} historical snapshot(s)
          </span>
        </div>
        {alerts.length === 0 ? (
          <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>
            Alerts appear here when generated reports match a saved subscription.
          </p>
        ) : (
          <div style={{ display: 'grid', gap: 8 }}>
            {alerts.slice(0, 12).map((alert) => (
              <div key={alert.id} style={{ display: 'flex', justifyContent: 'space-between', gap: 10, alignItems: 'start', borderTop: '1px solid var(--border-subtle)', paddingTop: 8 }}>
                <div>
                  <span className={`of-chip ${statusClass(alert.status)}`}>{healthReportStatusLabel(alert.status)}</span>
                  <strong style={{ display: 'block', marginTop: 6, fontSize: 13 }}>{alert.title}</strong>
                  <p style={{ margin: '4px 0 0', fontSize: 12 }}>{alert.body}</p>
                  <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                    {alert.channels.join(', ')} | {formatDate(alert.created_at)}
                  </p>
                </div>
                {!alert.acknowledged_at && (
                  <button type="button" className="of-button" onClick={() => acknowledge(alert.id)}>Acknowledge</button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>
    </section>
  );
}

export function ResourceHealthStatusBadge({ resourceRid, compact = false }: ResourceHealthStatusBadgeProps) {
  const reports = useMemo(() => (resourceRid ? latestReportsForResource(resourceRid) : []), [resourceRid]);
  const status = rollupHealthReportStatus(reports);
  const label = compact ? healthReportStatusLabel(status) : `Health ${healthReportStatusLabel(status)}`;
  return (
    <span className={`of-chip ${statusClass(status)}`} title={`${reports.length} latest check report(s)`}>
      {label}
    </span>
  );
}

export function ProjectHealthSummary({ resourceRids, title = 'Project Data Health' }: ProjectHealthSummaryProps) {
  const stableRids = useMemo(() => Array.from(new Set(resourceRids.filter(Boolean))), [resourceRids]);
  const reports = useMemo(() => latestReportsForResources(stableRids), [stableRids]);
  const summary = summarizeReports(reports);
  const status = rollupHealthReportStatus(reports);

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'start', flexWrap: 'wrap' }}>
        <div>
          <p className="of-eyebrow">Data Health</p>
          <h2 className="of-heading-md" style={{ marginTop: 4 }}>{title}</h2>
          <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 13 }}>
            Latest generated health status across {stableRids.length.toLocaleString()} project resource(s).
          </p>
        </div>
        <span className={`of-chip ${statusClass(status)}`}>{healthReportStatusLabel(status)}</span>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: 8 }}>
        <ReportStat label="Overall" value={healthReportStatusLabel(status)} status={status} />
        <ReportStat label="Failing" value={summary.failing.toLocaleString()} status="failing" />
        <ReportStat label="Warning" value={summary.warning.toLocaleString()} status="warning" />
        <ReportStat label="Passing" value={summary.passing.toLocaleString()} status="passing" />
      </div>
    </section>
  );
}

function ReportStat({ label, value, status = 'unknown' }: { label: string; value: string; status?: HealthReportStatus }) {
  return (
    <article className="of-panel-muted" style={{ padding: 10, display: 'grid', gap: 4 }}>
      <span className="of-text-muted" style={{ fontSize: 12 }}>{label}</span>
      <strong className={statusClass(status)} style={{ fontSize: 18, width: 'fit-content' }}>{value}</strong>
    </article>
  );
}

function ReportRow({ report }: { report: HealthCheckReport }) {
  return (
    <div style={{ display: 'grid', gap: 4, borderTop: '1px solid var(--border-subtle)', paddingTop: 8 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
        <strong style={{ fontSize: 13 }}>{report.resource_name}</strong>
        <span className={`of-chip ${statusClass(report.status)}`}>{healthReportStatusLabel(report.status)}</span>
      </div>
      <p style={{ margin: 0, fontSize: 12 }}>
        {resourceHealthCheckLabel(report.kind)} | {report.message}
      </p>
      <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
        {report.monitoring_view || 'No monitoring view'} | evaluated {formatDate(report.evaluated_at)}
      </p>
    </div>
  );
}

function summarizeReports(reports: HealthCheckReport[]) {
  return reports.reduce<Record<HealthReportStatus, number>>((summary, report) => {
    summary[report.status] += 1;
    return summary;
  }, { passing: 0, warning: 0, failing: 0, unknown: 0 });
}

function statusClass(status: HealthReportStatus) {
  if (status === 'passing') return 'of-status-success';
  if (status === 'warning') return 'of-status-warning';
  if (status === 'failing') return 'of-status-danger';
  return 'of-status-info';
}

function formatDate(value: string | null | undefined) {
  if (!value) return 'Never';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date);
}
