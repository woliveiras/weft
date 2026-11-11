/**
 * Link into Grafana Explore for a trace id, using the Tempo datasource
 * provisioned by deploy/ (uid "tempo").
 */
export function tempoLink(grafanaUrl: string, traceId: string): string {
  const left = {
    datasource: 'tempo',
    queries: [{ refId: 'A', datasource: { type: 'tempo', uid: 'tempo' }, queryType: 'traceql', query: traceId }],
    range: { from: 'now-24h', to: 'now' },
  };
  return `${grafanaUrl.replace(/\/$/, '')}/explore?orgId=1&left=${encodeURIComponent(JSON.stringify(left))}`;
}
