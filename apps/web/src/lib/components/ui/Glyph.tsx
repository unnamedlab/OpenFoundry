export type GlyphName =
  | 'menu' | 'home' | 'folder' | 'folder-open' | 'history' | 'search' | 'graph' | 'list' | 'cube'
  | 'database' | 'sparkles' | 'code' | 'settings' | 'ontology' | 'query' | 'bell'
  | 'help' | 'users' | 'chevron-down' | 'chevron-up' | 'chevron-right' | 'chevron-left' | 'plus'
  | 'x' | 'logout' | 'bookmark' | 'object' | 'link' | 'artifact' | 'run' | 'image'
  | 'audio' | 'video' | 'document' | 'spreadsheet' | 'email' | 'app' | 'check'
  | 'tag' | 'trash' | 'star' | 'star-filled' | 'eye' | 'lock' | 'shield' | 'shield-plus'
  | 'external-link' | 'info' | 'duplicate' | 'asterisk' | 'autosaved' | 'cover-page'
  | 'pie-chart' | 'project' | 'undo' | 'circle-x' | 'add-user' | 'tour' | 'pencil'
  | 'move' | 'badge-check' | 'view-grid' | 'login';

interface GlyphProps {
  name?: GlyphName;
  size?: number;
  strokeWidth?: number;
  tone?: string | null;
  filled?: boolean;
}

const FILLED_BY_DEFAULT: Record<string, true> = {
  'star-filled': true,
};

const SCHEMA_TONE: Record<string, string> = {
  image: '#7c3aed',
  audio: '#0ea5e9',
  video: '#ef4444',
  document: '#0891b2',
  spreadsheet: '#16a34a',
  email: '#f59e0b',
};

const PATHS: Record<string, string[]> = {
  menu: ['M4 6h16', 'M4 12h16', 'M4 18h16'],
  home: ['M4 11.5 12 5l8 6.5', 'M6 10.5V20h12v-9.5'],
  folder: ['M3.5 7.5h6l2 2h9v9a1.5 1.5 0 0 1-1.5 1.5H5A1.5 1.5 0 0 1 3.5 18.5z'],
  history: ['M4 12a8 8 0 1 0 2.3-5.7', 'M4 5v4h4', 'M12 8v4l2.5 1.5'],
  search: ['M10.5 18a7.5 7.5 0 1 1 5.3-2.2L20 20'],
  graph: ['M6 6h.01', 'M18 6h.01', 'M12 18h.01', 'M7 6.8l4.2 9.1', 'M17 6.8l-4.2 9.1', 'M8 6h8'],
  list: ['M8 7h11', 'M8 12h11', 'M8 17h11', 'M5 7h.01', 'M5 12h.01', 'M5 17h.01'],
  cube: ['M12 3.8 19 7.8v8.3l-7 4-7-4V7.8z', 'M12 12l7-4.2', 'M12 12 5 7.8', 'M12 12v8'],
  database: ['M5 7c0-1.7 3.1-3 7-3s7 1.3 7 3-3.1 3-7 3-7-1.3-7-3z', 'M5 7v5c0 1.7 3.1 3 7 3s7-1.3 7-3V7', 'M5 12v5c0 1.7 3.1 3 7 3s7-1.3 7-3v-5'],
  sparkles: ['M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z', 'M18 3v3', 'M19.5 4.5h-3', 'M4.5 16v2.5', 'M5.75 17.25H3.25'],
  code: ['M9 7 5 12l4 5', 'M15 7l4 5-4 5', 'M13 4l-2 16'],
  settings: ['M12 8.5A3.5 3.5 0 1 0 12 15.5 3.5 3.5 0 0 0 12 8.5Z', 'M12 2.8v2.1', 'M12 19.1v2.1', 'M4.8 4.8l1.5 1.5', 'M17.7 17.7l1.5 1.5', 'M2.8 12h2.1', 'M19.1 12h2.1', 'M4.8 19.2l1.5-1.5', 'M17.7 6.3l1.5-1.5'],
  ontology: ['M7 5.5h4v4H7z', 'M13 14.5h4v4h-4z', 'M13 5.5h4v4h-4z', 'M9 9.5v3h6v2', 'M15 9.5v5'],
  query: ['M4 6h16', 'M4 12h10', 'M4 18h7', 'M17.5 16.5 20 19', 'M18 13.5a3 3 0 1 1 0 6 3 3 0 0 1 0-6z'],
  bell: ['M12 4a4 4 0 0 1 4 4v2.8c0 .8.3 1.5.8 2.1l1.1 1.3H6.1l1.1-1.3c.5-.6.8-1.3.8-2.1V8a4 4 0 0 1 4-4z', 'M10 18a2 2 0 0 0 4 0'],
  help: ['M12 17h.01', 'M9.4 9a2.6 2.6 0 1 1 4.1 2.1c-.9.6-1.5 1.1-1.5 2.4'],
  users: ['M9 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6z', 'M16 10a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z', 'M4.8 18.5a4.2 4.2 0 0 1 8.4 0', 'M13.2 18.5a3.3 3.3 0 0 1 6.1-1.7'],
  'chevron-down': ['M6 9l6 6 6-6'],
  'chevron-up': ['M6 15l6-6 6 6'],
  'chevron-right': ['M9 6l6 6-6 6'],
  'chevron-left': ['M15 6l-6 6 6 6'],
  app: ['M3.5 5.5h17v11h-17z', 'M9 19h6', 'M12 16.5v2.5'],
  check: ['M5 12.5l4 4 10-10'],
  tag: ['M4 12V4.5h7.5L20 13l-7.5 7.5L4 12z', 'M8.5 8.5h.01'],
  trash: ['M5 7h14', 'M9 7V5h6v2', 'M7 7l1 12h8l1-12', 'M10 11v5', 'M14 11v5'],
  plus: ['M12 5v14', 'M5 12h14'],
  x: ['M6 6l12 12', 'M18 6 6 18'],
  logout: ['M10 5H6a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h4', 'M14 16l4-4-4-4', 'M9 12h9'],
  bookmark: ['M7 4.5h10v15l-5-3-5 3z'],
  object: ['M5 6h14v12H5z', 'M8 9h8', 'M8 12h8', 'M8 15h5'],
  link: ['M10 8.5 8.5 7A3.2 3.2 0 0 0 4 11.5 3.2 3.2 0 0 0 7.2 14.7l1.8-1.8', 'M14 15.5 15.5 17A3.2 3.2 0 0 0 20 12.5 3.2 3.2 0 0 0 16.8 9.3L15 11.1', 'M9 15l6-6'],
  artifact: ['M7 4.5h7l4 4v11H7z', 'M14 4.5v4h4'],
  run: ['M8 6.5v11l9-5.5z'],
  image: ['M4.5 5.5h15v13h-15z', 'M4.5 15.5l4-4 4 4 3-3 4 4', 'M9 9.5a1.4 1.4 0 1 1 0-2.8 1.4 1.4 0 0 1 0 2.8z'],
  audio: ['M9 18V8l8-3v10', 'M9 18a2 2 0 1 1-4 0 2 2 0 0 1 4 0z', 'M17 15a2 2 0 1 1-4 0 2 2 0 0 1 4 0z'],
  video: ['M4.5 6.5h11v11h-11z', 'M15.5 9.5l4-2v9l-4-2z'],
  document: ['M7 4.5h7l4 4v11H7z', 'M14 4.5v4h4', 'M9 13h6', 'M9 16h6', 'M9 10h3'],
  spreadsheet: ['M4.5 5.5h15v13h-15z', 'M4.5 9.5h15', 'M4.5 13.5h15', 'M9.5 5.5v13', 'M14.5 5.5v13'],
  email: ['M4.5 6.5h15v11h-15z', 'M4.5 7l7.5 6 7.5-6'],
  'folder-open': ['M3.5 7.5h6l2 2h9v3l-1.5 5.5a1 1 0 0 1-1 .8H4.6a1 1 0 0 1-1-.9L3.5 7.5z'],
  star: ['M12 4l2.4 5 5.6.8-4 4 .9 5.6L12 16.8l-5 2.6.9-5.6-4-4 5.6-.8z'],
  'star-filled': ['M12 4l2.4 5 5.6.8-4 4 .9 5.6L12 16.8l-5 2.6.9-5.6-4-4 5.6-.8z'],
  eye: ['M2.5 12c2.2-4.5 5.7-7 9.5-7s7.3 2.5 9.5 7c-2.2 4.5-5.7 7-9.5 7s-7.3-2.5-9.5-7z', 'M12 9.2a2.8 2.8 0 1 0 0 5.6 2.8 2.8 0 0 0 0-5.6z'],
  lock: ['M5.5 11h13v9h-13z', 'M8 11V8a4 4 0 0 1 8 0v3'],
  shield: ['M12 3.5l7.5 2.5v6.4c0 4-2.9 6.7-7.5 8.1-4.6-1.4-7.5-4.1-7.5-8.1V6z'],
  'shield-plus': ['M12 3.5l7.5 2.5v6.4c0 4-2.9 6.7-7.5 8.1-4.6-1.4-7.5-4.1-7.5-8.1V6z', 'M12 9v6', 'M9 12h6'],
  'external-link': ['M14 4.5h5.5V10', 'M19.5 4.5l-9 9', 'M19.5 13.5v5h-15v-15h5'],
  info: ['M12 4.5a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15z', 'M12 11v5', 'M12 8.2v.01'],
  duplicate: ['M8 8h11v11h-11z', 'M5 5h11v3', 'M5 5v11h3'],
  asterisk: ['M12 5v14', 'M5.5 8.4l13 7.2', 'M5.5 15.6l13-7.2'],
  autosaved: ['M7 4.5h7l4 4v11H7z', 'M14 4.5v4h4', 'M9.5 13.5l2 2 3.5-3.5'],
  'cover-page': ['M4 6c2-1 5-1.5 8-1.5s6 .5 8 1.5v13c-2-1-5-1.5-8-1.5s-6 .5-8 1.5z', 'M12 4.5v15'],
  'pie-chart': ['M12 4.5a7.5 7.5 0 1 0 7.5 7.5H12z', 'M12 4.5v7.5h7.5'],
  project: ['M5 7.5h14V19H5z', 'M9 7.5V5h6v2.5', 'M5 11.5h14'],
  undo: ['M4 12a8 8 0 1 1 2.3 5.7', 'M4 5v4h4'],
  'circle-x': ['M12 4.5a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15z', 'M9 9l6 6', 'M15 9l-6 6'],
  'add-user': ['M9 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6z', 'M3.5 19a5.5 5.5 0 0 1 11 0', 'M17 8v6', 'M14 11h6'],
  tour: ['M12 4.5l1.6 4 4.4.6-3.2 3.1.8 4.3-3.6-2-3.6 2 .8-4.3-3.2-3.1 4.4-.6z', 'M18 5.2v.8', 'M5.5 16.5v.8', 'M19 14.5h.8', 'M4.2 8.5h.8'],
  'badge-check': ['M12 4.5l2 1.5 2.4-.4.8 2.3 2.1 1.3-.7 2.3.7 2.3-2.1 1.3-.8 2.3-2.4-.4L12 19.5l-2-1.5-2.4.4-.8-2.3-2.1-1.3.7-2.3-.7-2.3 2.1-1.3.8-2.3 2.4.4z', 'M9 12l2 2 4-4'],
  'view-grid': ['M4 4.5h6v6h-6z', 'M14 4.5h6v6h-6z', 'M4 14.5h6v6h-6z', 'M14 14.5h6v6h-6z'],
  pencil: ['M4 20l1-4 11-11 3 3-11 11z', 'M14 7l3 3'],
  move: ['M12 4.5v15', 'M4.5 12h15', 'M9 7l3-3 3 3', 'M9 17l3 3 3-3', 'M7 9l-3 3 3 3', 'M17 9l3 3-3 3'],
  login: ['M14 5h4a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2h-4', 'M10 8l-4 4 4 4', 'M15 12H6'],
};

export function Glyph({ name = 'cube', size = 18, strokeWidth = 1.8, tone = null, filled }: GlyphProps) {
  const stroke = tone ?? SCHEMA_TONE[name] ?? 'currentColor';
  const paths = PATHS[name] ?? PATHS.cube;
  const isFilled = filled ?? FILLED_BY_DEFAULT[name] ?? false;
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill={isFilled ? stroke : 'none'} xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
      {paths.map((d, i) => (
        <path key={i} d={d} stroke={stroke} strokeWidth={strokeWidth} strokeLinecap="round" strokeLinejoin="round" />
      ))}
    </svg>
  );
}
