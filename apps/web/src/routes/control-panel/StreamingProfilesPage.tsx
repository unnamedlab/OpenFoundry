// Streaming-profile admin surface (CTRL-002). Backed by the parked
// /control-panel/streaming-profiles* endpoints in identity-federation-service
// per ADR-0046; will follow the resource to ingestion-replication-service
// when ADR-0035 P3 is picked back up.

import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';

import {
	createStreamingProfile,
	deleteStreamingProfile,
	listStreamingProfiles,
	pauseStreamingProfile,
	resumeStreamingProfile,
	updateStreamingProfile,
	STREAMING_PROFILE_CONNECTOR_TYPES,
	STREAMING_PROFILE_STATUSES,
	STREAMING_PROFILE_WATERMARK_POLICIES,
	type CreateStreamingProfileRequest,
	type StreamingProfile,
	type StreamingProfileConnectorType,
	type StreamingProfileStatus,
	type StreamingProfileWatermarkPolicy,
	type UpdateStreamingProfileRequest,
} from '@/lib/api/control-panel';
import { ConfirmDialog } from '@/lib/components/ConfirmDialog';

const CONNECTOR_LABELS: Record<StreamingProfileConnectorType, string> = Object.fromEntries(
	STREAMING_PROFILE_CONNECTOR_TYPES.map((c) => [c.value, c.label]),
) as Record<StreamingProfileConnectorType, string>;

const STATUS_TONES: Record<StreamingProfileStatus, { background: string; color: string }> = {
	active: { background: '#dcfce7', color: '#166534' },
	paused: { background: '#fef9c3', color: '#854d0e' },
	error: { background: '#fee2e2', color: '#991b1b' },
	draft: { background: '#e0e7ff', color: '#3730a3' },
};

interface ProfileFormState {
	id: string;
	name: string;
	description: string;
	connector_type: StreamingProfileConnectorType;
	status: StreamingProfileStatus;
	parallelism: string;
	watermark_policy: StreamingProfileWatermarkPolicy;
	checkpoint_interval_ms: string;
	source_config: string;
	destination_dataset_id: string;
}

const EMPTY_FORM: ProfileFormState = {
	id: '',
	name: '',
	description: '',
	connector_type: 'streaming_kafka',
	status: 'draft',
	parallelism: '1',
	watermark_policy: 'none',
	checkpoint_interval_ms: '0',
	source_config: '{}',
	destination_dataset_id: '',
};

function formToCreate(form: ProfileFormState): CreateStreamingProfileRequest {
	return {
		name: form.name.trim(),
		description: form.description.trim() || undefined,
		connector_type: form.connector_type,
		status: form.status,
		parallelism: Number(form.parallelism) || 0,
		watermark_policy: form.watermark_policy,
		checkpoint_interval_ms: Number(form.checkpoint_interval_ms) || 0,
		source_config: JSON.parse(form.source_config),
		destination_dataset_id: form.destination_dataset_id.trim() || undefined,
	};
}

function formToUpdate(form: ProfileFormState): UpdateStreamingProfileRequest {
	return {
		name: form.name.trim(),
		description: form.description.trim(),
		connector_type: form.connector_type,
		parallelism: Number(form.parallelism) || 0,
		watermark_policy: form.watermark_policy,
		checkpoint_interval_ms: Number(form.checkpoint_interval_ms) || 0,
		source_config: JSON.parse(form.source_config),
		destination_dataset_id: form.destination_dataset_id.trim(),
	};
}

function profileToForm(profile: StreamingProfile): ProfileFormState {
	return {
		id: profile.id,
		name: profile.name,
		description: profile.description ?? '',
		connector_type: profile.connector_type,
		status: profile.status,
		parallelism: String(profile.parallelism),
		watermark_policy: profile.watermark_policy,
		checkpoint_interval_ms: String(profile.checkpoint_interval_ms),
		source_config: JSON.stringify(profile.source_config ?? {}, null, 2),
		destination_dataset_id: profile.destination_dataset_id ?? '',
	};
}

function formatTimestamp(value?: string) {
	if (!value) return '—';
	const d = new Date(value);
	return Number.isNaN(d.getTime()) ? value : d.toLocaleString();
}

function formatThroughput(value?: number) {
	if (value == null) return '—';
	return `${value.toFixed(1)} eps`;
}

export function StreamingProfilesPage() {
	const [profiles, setProfiles] = useState<StreamingProfile[]>([]);
	const [statusFilter, setStatusFilter] = useState<'' | StreamingProfileStatus>('');
	const [connectorFilter, setConnectorFilter] = useState<'' | StreamingProfileConnectorType>('');
	const [loading, setLoading] = useState(true);
	const [busyAction, setBusyAction] = useState<string>('');
	const [error, setError] = useState('');
	const [modalState, setModalState] = useState<
		| { mode: 'closed' }
		| { mode: 'create' }
		| { mode: 'edit'; profile: StreamingProfile }
	>({ mode: 'closed' });
	const [deleteTarget, setDeleteTarget] = useState<StreamingProfile | null>(null);

	const refresh = useCallback(async () => {
		setLoading(true);
		setError('');
		try {
			const res = await listStreamingProfiles({
				status: statusFilter || undefined,
				connector_type: connectorFilter || undefined,
			});
			setProfiles(res.items);
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : 'Failed to load streaming profiles');
		} finally {
			setLoading(false);
		}
	}, [statusFilter, connectorFilter]);

	useEffect(() => {
		void refresh();
	}, [refresh]);

	async function toggleStatus(profile: StreamingProfile) {
		const next = profile.status === 'paused' ? 'resume' : 'pause';
		setBusyAction(`${next}-${profile.id}`);
		setError('');
		try {
			if (next === 'pause') {
				await pauseStreamingProfile(profile.id);
			} else {
				await resumeStreamingProfile(profile.id);
			}
			await refresh();
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : `Failed to ${next} profile`);
		} finally {
			setBusyAction('');
		}
	}

	async function performDelete() {
		if (!deleteTarget) return;
		setBusyAction(`delete-${deleteTarget.id}`);
		setError('');
		try {
			await deleteStreamingProfile(deleteTarget.id);
			setDeleteTarget(null);
			await refresh();
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : 'Failed to delete profile');
		} finally {
			setBusyAction('');
		}
	}

	const modalOpen = modalState.mode !== 'closed';

	return (
		<section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
			<Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
				← Control panel
			</Link>

			<header style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16 }}>
				<div>
					<h1 className="of-heading-xl">Streaming profiles</h1>
					<p className="of-text-muted" style={{ marginTop: 4, maxWidth: 760 }}>
						Reusable streaming-pipeline templates: connector type, parallelism, watermark policy and
						source wiring. Profiles are referenced from pipeline <code>StreamingConfig</code>. Last-event
						and throughput metrics are best-effort and stay empty until the telemetry job lands
						(ADR-0046).
					</p>
				</div>
				<button
					type="button"
					className="of-btn of-btn-primary"
					onClick={() => setModalState({ mode: 'create' })}
				>
					+ New profile
				</button>
			</header>

			{error && (
				<div
					role="alert"
					className="of-status-danger"
					style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}
				>
					{error}
				</div>
			)}

			<section className="of-panel" style={{ padding: 12, display: 'flex', gap: 12, flexWrap: 'wrap' }}>
				<label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
					<span className="of-text-muted">Status</span>
					<select
						aria-label="Status filter"
						value={statusFilter}
						onChange={(e) => setStatusFilter(e.target.value as '' | StreamingProfileStatus)}
					>
						<option value="">All</option>
						{STREAMING_PROFILE_STATUSES.map((s) => (
							<option key={s.value} value={s.value}>
								{s.label}
							</option>
						))}
					</select>
				</label>
				<label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
					<span className="of-text-muted">Connector type</span>
					<select
						aria-label="Connector type filter"
						value={connectorFilter}
						onChange={(e) =>
							setConnectorFilter(e.target.value as '' | StreamingProfileConnectorType)
						}
					>
						<option value="">All</option>
						{STREAMING_PROFILE_CONNECTOR_TYPES.map((c) => (
							<option key={c.value} value={c.value}>
								{c.label}
							</option>
						))}
					</select>
				</label>
			</section>

			<section className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
				<table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
					<thead style={{ background: 'var(--bg-subtle)', textAlign: 'left' }}>
						<tr>
							<th style={{ padding: 10 }}>Name</th>
							<th style={{ padding: 10 }}>Connector</th>
							<th style={{ padding: 10 }}>Status</th>
							<th style={{ padding: 10 }}>Last event</th>
							<th style={{ padding: 10 }}>Throughput</th>
							<th style={{ padding: 10 }}>Updated</th>
							<th style={{ padding: 10 }}>Actions</th>
						</tr>
					</thead>
					<tbody>
						{loading && (
							<tr>
								<td colSpan={7} style={{ padding: 12 }} className="of-text-muted">
									Loading streaming profiles…
								</td>
							</tr>
						)}
						{!loading && profiles.length === 0 && (
							<tr>
								<td colSpan={7} style={{ padding: 16 }} className="of-text-muted">
									No streaming profiles yet. Create one to start importing data into pipelines.
								</td>
							</tr>
						)}
						{!loading &&
							profiles.map((profile) => (
								<tr key={profile.id} style={{ borderTop: '1px solid var(--border-subtle)' }}>
									<td style={{ padding: 10 }}>
										<button
											type="button"
											onClick={() => setModalState({ mode: 'edit', profile })}
											style={{
												background: 'transparent',
												border: 'none',
												padding: 0,
												color: 'var(--text-accent)',
												cursor: 'pointer',
												textDecoration: 'underline',
												font: 'inherit',
											}}
										>
											{profile.name}
										</button>
										{profile.description && (
											<div className="of-text-muted" style={{ fontSize: 11 }}>
												{profile.description}
											</div>
										)}
									</td>
									<td style={{ padding: 10 }}>{CONNECTOR_LABELS[profile.connector_type]}</td>
									<td style={{ padding: 10 }}>
										<span
											style={{
												display: 'inline-block',
												padding: '2px 8px',
												borderRadius: 999,
												fontSize: 11,
												fontWeight: 600,
												...STATUS_TONES[profile.status],
											}}
										>
											{profile.status}
										</span>
									</td>
									<td style={{ padding: 10 }}>{formatTimestamp(profile.last_event_at)}</td>
									<td style={{ padding: 10 }}>{formatThroughput(profile.throughput_eps)}</td>
									<td style={{ padding: 10 }}>{formatTimestamp(profile.updated_at)}</td>
									<td style={{ padding: 10 }}>
										<div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
											<button
												type="button"
												className="of-btn of-btn-ghost"
												onClick={() => setModalState({ mode: 'edit', profile })}
											>
												Edit
											</button>
											<button
												type="button"
												className="of-btn of-btn-ghost"
												onClick={() => void toggleStatus(profile)}
												disabled={
													busyAction === `pause-${profile.id}` ||
													busyAction === `resume-${profile.id}` ||
													profile.status === 'error'
												}
											>
												{profile.status === 'paused' ? 'Resume' : 'Pause'}
											</button>
											<button
												type="button"
												className="of-btn of-btn-ghost"
												style={{ color: '#b91c1c' }}
												onClick={() => setDeleteTarget(profile)}
											>
												Delete
											</button>
										</div>
									</td>
								</tr>
							))}
					</tbody>
				</table>
			</section>

			{modalOpen && (
				<ProfileFormModal
					initial={
						modalState.mode === 'edit' ? profileToForm(modalState.profile) : EMPTY_FORM
					}
					mode={modalState.mode === 'edit' ? 'edit' : 'create'}
					onClose={() => setModalState({ mode: 'closed' })}
					onSubmit={async (form) => {
						setError('');
						if (modalState.mode === 'edit') {
							await updateStreamingProfile(modalState.profile.id, formToUpdate(form));
						} else {
							await createStreamingProfile(formToCreate(form));
						}
						setModalState({ mode: 'closed' });
						await refresh();
					}}
				/>
			)}

			<ConfirmDialog
				open={deleteTarget !== null}
				title="Delete streaming profile"
				message={
					deleteTarget
						? `Delete "${deleteTarget.name}"? Pipelines that reference this profile id will fail until reattached.`
						: ''
				}
				confirmLabel="Delete"
				danger
				busy={busyAction.startsWith('delete-')}
				onConfirm={() => void performDelete()}
				onCancel={() => setDeleteTarget(null)}
			/>
		</section>
	);
}

interface ProfileFormModalProps {
	initial: ProfileFormState;
	mode: 'create' | 'edit';
	onClose: () => void;
	onSubmit: (form: ProfileFormState) => Promise<void>;
}

function ProfileFormModal({ initial, mode, onClose, onSubmit }: ProfileFormModalProps) {
	const [form, setForm] = useState<ProfileFormState>(initial);
	const [busy, setBusy] = useState(false);
	const [formError, setFormError] = useState('');

	const title = mode === 'create' ? 'New streaming profile' : 'Edit streaming profile';

	const sourceConfigInvalid = useMemo(() => {
		try {
			const parsed = JSON.parse(form.source_config);
			if (parsed === null || Array.isArray(parsed) || typeof parsed !== 'object') {
				return 'source_config must be a JSON object';
			}
			return '';
		} catch {
			return 'source_config must be valid JSON';
		}
	}, [form.source_config]);

	async function handleSubmit(e: FormEvent) {
		e.preventDefault();
		setFormError('');
		if (!form.name.trim()) {
			setFormError('Name is required');
			return;
		}
		if (sourceConfigInvalid) {
			setFormError(sourceConfigInvalid);
			return;
		}
		setBusy(true);
		try {
			await onSubmit(form);
		} catch (cause) {
			setFormError(cause instanceof Error ? cause.message : 'Failed to save profile');
		} finally {
			setBusy(false);
		}
	}

	return (
		<div
			role="dialog"
			aria-modal="true"
			aria-labelledby="streaming-profile-modal-title"
			style={{
				position: 'fixed',
				inset: 0,
				zIndex: 40,
				display: 'flex',
				alignItems: 'flex-start',
				justifyContent: 'center',
				background: 'rgba(0,0,0,0.4)',
				padding: 24,
				overflowY: 'auto',
			}}
		>
			<form
				onSubmit={handleSubmit}
				className="of-panel"
				style={{
					width: '100%',
					maxWidth: 640,
					background: '#fff',
					display: 'grid',
					gap: 0,
				}}
			>
				<div style={{ borderBottom: '1px solid var(--border-default)', padding: '12px 16px' }}>
					<div id="streaming-profile-modal-title" className="of-heading-sm">
						{title}
					</div>
				</div>
				<div style={{ padding: 16, display: 'grid', gap: 12 }}>
					{formError && (
						<div
							role="alert"
							className="of-status-danger"
							style={{ padding: '8px 12px', borderRadius: 'var(--radius-md)', fontSize: 13 }}
						>
							{formError}
						</div>
					)}
					<label style={{ display: 'grid', gap: 4 }}>
						<span style={{ fontSize: 12 }}>Name</span>
						<input
							required
							value={form.name}
							onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
						/>
					</label>
					<label style={{ display: 'grid', gap: 4 }}>
						<span style={{ fontSize: 12 }}>Description</span>
						<input
							value={form.description}
							onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
						/>
					</label>
					<div style={{ display: 'grid', gap: 12, gridTemplateColumns: '1fr 1fr' }}>
						<label style={{ display: 'grid', gap: 4 }}>
							<span style={{ fontSize: 12 }}>Connector type</span>
							<select
								value={form.connector_type}
								onChange={(e) =>
									setForm((f) => ({
										...f,
										connector_type: e.target.value as StreamingProfileConnectorType,
									}))
								}
							>
								{STREAMING_PROFILE_CONNECTOR_TYPES.map((c) => (
									<option key={c.value} value={c.value}>
										{c.label}
									</option>
								))}
							</select>
						</label>
						{mode === 'create' ? (
							<label style={{ display: 'grid', gap: 4 }}>
								<span style={{ fontSize: 12 }}>Initial status</span>
								<select
									value={form.status}
									onChange={(e) =>
										setForm((f) => ({ ...f, status: e.target.value as StreamingProfileStatus }))
									}
								>
									{STREAMING_PROFILE_STATUSES.map((s) => (
										<option key={s.value} value={s.value}>
											{s.label}
										</option>
									))}
								</select>
							</label>
						) : (
							<div style={{ display: 'grid', gap: 4 }}>
								<span style={{ fontSize: 12 }} className="of-text-muted">
									Status (use pause/resume to change)
								</span>
								<span style={{ fontSize: 13, padding: '6px 0' }}>{form.status}</span>
							</div>
						)}
					</div>
					<div style={{ display: 'grid', gap: 12, gridTemplateColumns: '1fr 1fr' }}>
						<label style={{ display: 'grid', gap: 4 }}>
							<span style={{ fontSize: 12 }}>Parallelism</span>
							<input
								type="number"
								min={0}
								value={form.parallelism}
								onChange={(e) => setForm((f) => ({ ...f, parallelism: e.target.value }))}
							/>
						</label>
						<label style={{ display: 'grid', gap: 4 }}>
							<span style={{ fontSize: 12 }}>Checkpoint interval (ms)</span>
							<input
								type="number"
								min={0}
								value={form.checkpoint_interval_ms}
								onChange={(e) =>
									setForm((f) => ({ ...f, checkpoint_interval_ms: e.target.value }))
								}
							/>
						</label>
					</div>
					<label style={{ display: 'grid', gap: 4 }}>
						<span style={{ fontSize: 12 }}>Watermark policy</span>
						<select
							value={form.watermark_policy}
							onChange={(e) =>
								setForm((f) => ({
									...f,
									watermark_policy: e.target.value as StreamingProfileWatermarkPolicy,
								}))
							}
						>
							{STREAMING_PROFILE_WATERMARK_POLICIES.map((w) => (
								<option key={w.value} value={w.value}>
									{w.label}
								</option>
							))}
						</select>
					</label>
					<label style={{ display: 'grid', gap: 4 }}>
						<span style={{ fontSize: 12 }}>Destination dataset id</span>
						<input
							value={form.destination_dataset_id}
							onChange={(e) =>
								setForm((f) => ({ ...f, destination_dataset_id: e.target.value }))
							}
						/>
					</label>
					<label style={{ display: 'grid', gap: 4 }}>
						<span style={{ fontSize: 12 }}>
							Source config (JSON object) {sourceConfigInvalid && (
								<span style={{ color: '#b91c1c', marginLeft: 6 }}>{sourceConfigInvalid}</span>
							)}
						</span>
						<textarea
							rows={6}
							spellCheck={false}
							style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
							value={form.source_config}
							onChange={(e) => setForm((f) => ({ ...f, source_config: e.target.value }))}
						/>
					</label>
				</div>
				<div
					style={{
						display: 'flex',
						justifyContent: 'flex-end',
						gap: 8,
						borderTop: '1px solid var(--border-default)',
						padding: '12px 16px',
					}}
				>
					<button type="button" className="of-btn of-btn-ghost" onClick={onClose} disabled={busy}>
						Cancel
					</button>
					<button type="submit" className="of-btn of-btn-primary" disabled={busy}>
						{busy ? 'Saving…' : mode === 'create' ? 'Create' : 'Save'}
					</button>
				</div>
			</form>
		</div>
	);
}
