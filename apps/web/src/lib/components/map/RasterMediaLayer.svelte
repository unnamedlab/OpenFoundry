<script lang="ts">
	// H7 — Map raster-source helper for media-set-backed TIFF/NITF layers.
	//
	// Mirrors the `TileSourceDescriptor` produced server-side by the
	// `geospatial-tiles` Rust lib (libs/geospatial-tiles/src/lib.rs). The
	// goal is a single front-end primitive that turns a media-set RID
	// into a MapLibre-compatible raster source (the doc-canonical
	// `/tiles/{rid}/{z}/{x}/{y}.png` shape from the H7 spec).
	//
	// Usage in a route:
	//   <RasterMediaLayer mediaSetRid={rid} schema="DICOM" />
	// or override defaults explicitly:
	//   <RasterMediaLayer mediaSetRid={rid} tileSize={512} maxzoom={20} />

	type RasterMediaLayerProps = {
		mediaSetRid: string;
		/** Foundry MediaSetSchema. DICOM/IMAGE here serve TIFF/NITF/JPEG2000 tiles. */
		schema?: 'IMAGE' | 'DICOM' | 'DOCUMENT';
		tileSize?: 256 | 512;
		minzoom?: number;
		maxzoom?: number;
		/** Override path origin if tiles are served by a different gateway. */
		origin?: string;
	};

	let {
		mediaSetRid,
		schema = 'IMAGE',
		tileSize = 256,
		minzoom = 0,
		maxzoom = 22,
		origin = ''
	}: RasterMediaLayerProps = $props();

	const tileUrlTemplate = $derived(
		`${origin}/tiles/${mediaSetRid}/{z}/{x}/{y}.png`
	);

	// MapLibre raster `source` payload — copy/paste-able into a `<MapLibre>`
	// instance or `map.addSource(...)` call. Kept as a $derived object so
	// downstream components can subscribe to it.
	const mapLibreSource = $derived({
		type: 'raster' as const,
		tiles: [tileUrlTemplate],
		tileSize,
		minzoom,
		maxzoom,
		attribution: '© OpenFoundry media-sets-service · access pattern: geo_tile'
	});
</script>

<section
	class="rounded-3xl border border-stone-200 bg-white p-5 shadow-sm shadow-stone-200/60"
	data-testid="raster-media-layer"
>
	<header>
		<p class="text-xs font-semibold uppercase tracking-[0.24em] text-cyan-700">
			Raster media layer
		</p>
		<h3 class="mt-2 text-xl font-semibold text-stone-900">
			Tile source · {schema}
		</h3>
		<p class="mt-1 text-sm text-stone-500">
			Wires a MapLibre raster source against the
			<code class="rounded bg-stone-100 px-1 py-0.5 text-xs">geo_tile</code> access
			pattern of this media set.
		</p>
	</header>

	<div class="mt-5 grid gap-4 xl:grid-cols-2">
		<div class="rounded-2xl border border-stone-200 bg-stone-50 p-4 text-sm text-stone-600">
			<p class="font-semibold text-stone-900">Tile URL template</p>
			<p class="mt-2 break-all font-mono text-xs text-stone-500" data-testid="raster-media-layer-template">
				{tileUrlTemplate}
			</p>
			<dl class="mt-4 grid grid-cols-2 gap-2 text-xs text-stone-600">
				<div>
					<dt class="font-semibold text-stone-900">Tile size</dt>
					<dd>{tileSize} px</dd>
				</div>
				<div>
					<dt class="font-semibold text-stone-900">Zoom</dt>
					<dd>{minzoom}–{maxzoom}</dd>
				</div>
			</dl>
		</div>

		<div class="rounded-2xl border border-stone-200 bg-stone-50 p-4 text-sm text-stone-600">
			<p class="font-semibold text-stone-900">MapLibre source payload</p>
			<pre
				class="mt-2 max-h-64 overflow-auto rounded-2xl border border-stone-200 bg-white p-3 font-mono text-xs text-stone-700"
				data-testid="raster-media-layer-source"
			>{JSON.stringify(mapLibreSource, null, 2)}</pre>
		</div>
	</div>
</section>
