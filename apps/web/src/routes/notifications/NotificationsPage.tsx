import { Glyph } from '@/lib/components/ui/Glyph';

export function NotificationsPage() {
  return (
    <section style={{ padding: '24px 28px', display: 'grid', gap: 14 }}>
      <header style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <Glyph name="bell" size={18} />
        <h1 style={{ margin: 0, fontSize: 20, fontWeight: 600 }}>Notifications</h1>
      </header>
      <div className="of-panel" style={{ padding: 36, textAlign: 'center' }}>
        <p className="of-text-muted" style={{ margin: 0, fontSize: 14 }}>
          You have no notifications.
        </p>
      </div>
    </section>
  );
}
