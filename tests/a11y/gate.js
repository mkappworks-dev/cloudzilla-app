import { allowlist } from './allowlist.js';

export function unallowed(route, theme, violations) {
  return violations.filter(v => v.impact === 'serious' || v.impact === 'critical').flatMap(v =>
    v.nodes.filter(node => {
      // Counts and timestamps vary between fresh seeds; keep the surrounding selector exact.
      const target = node.target.join(' ').replace(/a\[aria-label="Notifications, \d+ unread"\]/, 'a[aria-label^="Notifications,"]').replace(/time\[datetime="[^"]+"\]/g, 'time[datetime]');
      return !allowlist.some(a => a.route === route && a.theme === theme && a.rule === v.id && a.target === target && a.reason);
    }).map(node => ({ rule: v.id, impact: v.impact, target: node.target.join(' '), summary: node.failureSummary })),
  );
}
