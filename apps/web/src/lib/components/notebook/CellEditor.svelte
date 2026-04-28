<script lang="ts">
	import { onMount } from 'svelte';
	import type * as Monaco from 'monaco-editor/esm/vs/editor/editor.api';

	interface Props {
		value: string;
		language: string;
		minHeight?: number;
		onChange?: (value: string) => void;
		onBlur?: (value: string) => void;
	}

	let {
		value,
		language,
		minHeight = 160,
		onChange,
		onBlur,
	}: Props = $props();

	let container = $state<HTMLDivElement | null>(null);
	let monaco = $state<typeof import('monaco-editor/esm/vs/editor/editor.api') | null>(null);
	let editor = $state<Monaco.editor.IStandaloneCodeEditor | null>(null);
	let syncing = false;
	let monacoEditorPromise: Promise<typeof import('monaco-editor/esm/vs/editor/editor.api')> | null = null;
	const loadedMonacoLanguages = new Set<string>();

	function resolveMonacoLanguage(input: string) {
		if (input === 'text') return 'plaintext';
		if (input === 'toml') return 'ini';
		return input;
	}

	function loadMonacoEditor() {
		monacoEditorPromise ??= import('monaco-editor/esm/vs/editor/editor.api');
		return monacoEditorPromise;
	}

	async function loadMonacoLanguage(input: string) {
		const language = resolveMonacoLanguage(input);
		if (loadedMonacoLanguages.has(language)) {
			return language;
		}

		switch (language) {
			case 'json':
				await import('monaco-editor/esm/vs/language/json/monaco.contribution');
				break;
			case 'javascript':
			case 'typescript':
				await import('monaco-editor/esm/vs/language/typescript/monaco.contribution');
				break;
			case 'markdown':
				await import('monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution');
				break;
			case 'python':
				await import('monaco-editor/esm/vs/basic-languages/python/python.contribution');
				break;
			case 'sql':
				await import('monaco-editor/esm/vs/basic-languages/sql/sql.contribution');
				break;
			case 'r':
				await import('monaco-editor/esm/vs/basic-languages/r/r.contribution');
				break;
			case 'ini':
				await import('monaco-editor/esm/vs/basic-languages/ini/ini.contribution');
				break;
		}

		loadedMonacoLanguages.add(language);
		return language;
	}

	onMount(() => {
		let changeSubscription: Monaco.IDisposable | null = null;
		let blurSubscription: Monaco.IDisposable | null = null;
		let disposed = false;

		async function initializeEditor() {
			const [editorApi, editorLanguage] = await Promise.all([
				loadMonacoEditor(),
				loadMonacoLanguage(language)
			]);

			monaco = editorApi;

			if (disposed || !container) {
				return;
			}

			const createdEditor = monaco.editor.create(container, {
				value,
				language: editorLanguage,
				automaticLayout: true,
				minimap: { enabled: false },
				fontSize: 13,
				lineNumbers: 'on',
				roundedSelection: false,
				scrollBeyondLastLine: false,
				wordWrap: editorLanguage === 'markdown' ? 'on' : 'off',
				theme: document.documentElement.classList.contains('dark') ? 'vs-dark' : 'vs',
			});

			if (disposed) {
				createdEditor.dispose();
				return;
			}

			editor = createdEditor;

			changeSubscription = createdEditor.onDidChangeModelContent(() => {
				if (syncing) {
					return;
				}
				onChange?.(createdEditor.getValue());
			});

			blurSubscription = createdEditor.onDidBlurEditorText(() => {
				onBlur?.(createdEditor.getValue());
			});
		}

		void initializeEditor();

		return () => {
			disposed = true;
			changeSubscription?.dispose();
			blurSubscription?.dispose();
			editor?.dispose();
			editor = null;
		};
	});

	$effect(() => {
		if (!editor) {
			return;
		}
		if (editor.getValue() === value) {
			return;
		}

		syncing = true;
		editor.setValue(value);
		syncing = false;
	});

	$effect(() => {
		if (!editor || !monaco) {
			return;
		}

		const model = editor.getModel();
		if (!model) {
			return;
		}

		let cancelled = false;

		void (async () => {
			const editorLanguage = await loadMonacoLanguage(language);
			if (cancelled) {
				return;
			}

			monaco.editor.setModelLanguage(model, editorLanguage);
			editor.updateOptions({
				wordWrap: editorLanguage === 'markdown' ? 'on' : 'off',
			});
		})();

		return () => {
			cancelled = true;
		};
	});
</script>

<div
	bind:this={container}
	class="w-full"
	style={`height: ${Math.max(minHeight, 96)}px;`}
></div>
