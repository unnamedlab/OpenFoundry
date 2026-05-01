/**
 * Global store for the Ontology Search command palette (T8).
 *
 * Any component can call `ontologySearch.open()` (optionally with an initial
 * query) to surface the palette. The palette listens to this store and to
 * Cmd/Ctrl+K so it can be triggered from anywhere in the app.
 */
import { writable } from 'svelte/store';

interface OntologySearchState {
  open: boolean;
  initialQuery: string;
}

const store = writable<OntologySearchState>({ open: false, initialQuery: '' });

export const ontologySearch = {
  subscribe: store.subscribe,
  open(initialQuery = '') {
    store.set({ open: true, initialQuery });
  },
  close() {
    store.update((state) => ({ ...state, open: false }));
  },
  toggle() {
    store.update((state) => ({ ...state, open: !state.open, initialQuery: '' }));
  },
};
