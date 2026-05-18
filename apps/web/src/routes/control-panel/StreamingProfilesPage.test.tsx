// @vitest-environment jsdom
import '@testing-library/jest-dom/vitest';

import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('react-router-dom', () => ({
	Link: ({ to, children, ...rest }: { to: string; children: ReactNode } & Record<string, unknown>) => (
		<a href={typeof to === 'string' ? to : '#'} {...rest}>
			{children}
		</a>
	),
}));

vi.mock('@/lib/api/control-panel', async () => {
	const actual = await vi.importActual<typeof import('@/lib/api/control-panel')>(
		'@/lib/api/control-panel',
	);
	return {
		...actual,
		listStreamingProfiles: vi.fn(),
		createStreamingProfile: vi.fn(),
		updateStreamingProfile: vi.fn(),
		deleteStreamingProfile: vi.fn(),
		pauseStreamingProfile: vi.fn(),
		resumeStreamingProfile: vi.fn(),
	};
});

import {
	createStreamingProfile,
	listStreamingProfiles,
	type StreamingProfile,
} from '@/lib/api/control-panel';
import { StreamingProfilesPage } from './StreamingProfilesPage';

const listMock = vi.mocked(listStreamingProfiles);
const createMock = vi.mocked(createStreamingProfile);

function makeProfile(overrides: Partial<StreamingProfile> = {}): StreamingProfile {
	return {
		id: 'profile-1',
		name: 'Kafka warm',
		connector_type: 'streaming_kafka',
		status: 'active',
		parallelism: 4,
		watermark_policy: 'bounded_out_of_orderness',
		checkpoint_interval_ms: 5000,
		source_config: { topic: 'events' },
		updated_at: '2026-05-17T10:00:00Z',
		...overrides,
	};
}

describe('StreamingProfilesPage', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	afterEach(() => {
		cleanup();
	});

	it('renders the empty state when no profiles exist', async () => {
		listMock.mockResolvedValue({ items: [], total: 0 });

		render(<StreamingProfilesPage />);

		await waitFor(() => {
			expect(listMock).toHaveBeenCalled();
		});

		expect(
			await screen.findByText(/No streaming profiles yet\./i),
		).toBeInTheDocument();
	});

	it('renders one row per profile with connector and status columns', async () => {
		listMock.mockResolvedValue({
			items: [
				makeProfile({ id: 'p-1', name: 'Kafka warm', connector_type: 'streaming_kafka' }),
				makeProfile({
					id: 'p-2',
					name: 'Kinesis east',
					connector_type: 'streaming_kinesis',
					status: 'paused',
				}),
			],
			total: 2,
		});

		render(<StreamingProfilesPage />);

		const rowKafka = await screen.findByRole('button', { name: 'Kafka warm' });
		const rowKinesis = await screen.findByRole('button', { name: 'Kinesis east' });
		expect(rowKafka).toBeInTheDocument();
		expect(rowKinesis).toBeInTheDocument();

		// Filter dropdowns also render the connector / status labels, so scope
		// to the actual data rows by looking at the row cells.
		const kafkaRow = rowKafka.closest('tr');
		const kinesisRow = rowKinesis.closest('tr');
		expect(kafkaRow).not.toBeNull();
		expect(kinesisRow).not.toBeNull();
		expect(within(kafkaRow as HTMLTableRowElement).getByText('Apache Kafka')).toBeInTheDocument();
		expect(within(kafkaRow as HTMLTableRowElement).getByText('active')).toBeInTheDocument();
		expect(within(kinesisRow as HTMLTableRowElement).getByText('Amazon Kinesis')).toBeInTheDocument();
		expect(within(kinesisRow as HTMLTableRowElement).getByText('paused')).toBeInTheDocument();
	});

	it('opens the create modal when "+ New profile" is clicked', async () => {
		listMock.mockResolvedValue({ items: [], total: 0 });

		render(<StreamingProfilesPage />);

		await waitFor(() => {
			expect(listMock).toHaveBeenCalled();
		});

		fireEvent.click(screen.getByRole('button', { name: '+ New profile' }));

		const dialog = await screen.findByRole('dialog');
		expect(within(dialog).getByText('New streaming profile')).toBeInTheDocument();
	});

	it('calls createStreamingProfile with the form values on submit', async () => {
		listMock.mockResolvedValue({ items: [], total: 0 });
		createMock.mockResolvedValue(
			makeProfile({ id: 'p-new', name: 'Brand new', status: 'draft' }),
		);

		render(<StreamingProfilesPage />);

		await waitFor(() => expect(listMock).toHaveBeenCalledTimes(1));

		fireEvent.click(screen.getByRole('button', { name: '+ New profile' }));

		const dialog = await screen.findByRole('dialog');
		// Name input is the only required text input the modal renders; query
		// the first textbox inside the dialog.
		const inputs = within(dialog).getAllByRole('textbox');
		const nameInput = inputs[0];
		fireEvent.change(nameInput, { target: { value: 'Brand new' } });

		fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

		await waitFor(() => {
			expect(createMock).toHaveBeenCalledTimes(1);
		});

		const call = createMock.mock.calls[0][0];
		expect(call.name).toBe('Brand new');
		expect(call.connector_type).toBe('streaming_kafka');
		expect(call.source_config).toEqual({});
	});

	it('surfaces a list error in an alert', async () => {
		listMock.mockRejectedValue(new Error('backend exploded'));

		render(<StreamingProfilesPage />);

		const alert = await screen.findByRole('alert');
		expect(alert).toHaveTextContent('backend exploded');
	});
});
