import {useEffect,useState} from 'react';
import {api} from '../api/client';
import type {Info} from '../api/types';
import {useAsync} from '../hooks/useData';
import {Badge,Btn} from './common';
import {AdminHeader} from './AdministrationUI';
export function IntegrationsView({info,infoError}:{info?:Info;infoError?:string}) {
 const health=useAsync(api.health,[]);const readiness=useAsync(api.ready,[]);const [checkedAt,setCheckedAt]=useState<string>();
 const loading=health.loading||readiness.loading;
 useEffect(()=>{if(!loading)setCheckedAt(new Date().toISOString());},[loading]);
 const checks=readiness.error?undefined:readiness.data;
 const services=[
 {name:'Control API',ok:!health.error&&health.data?.status==='ok',warning:false,unknown:!health.error&&!health.data,detail:health.error?`Health check failed: ${health.error}`:health.data?.status==='ok'?`Responding${info?` · ${info.instance} · version ${info.version}`:''}`:infoError??'Waiting for check',action:'Check the Weft service and server logs.'},
 {name:'Storage',ok:checks?.storage==='ok',warning:false,unknown:!checks,detail:checks?.storage??readiness.error??'Waiting for check',action:'Check storage connectivity and server logs.'},
 {name:'Queue',ok:checks?.queue==='ok',warning:false,unknown:!checks,detail:checks?.queue??readiness.error??'Waiting for check',action:'Check queue connectivity and server logs.'},
 {name:'Sandbox runtime',ok:checks?.sandbox==='ok',warning:checks?.sandbox==='not configured',unknown:!checks?.sandbox,detail:checks?.sandbox==='ok'?'Docker daemon dedicated to Weft is reachable. Agents with docker isolation run their commands there, never on the host.':checks?.sandbox==='not configured'?'No sandbox daemon is configured. Agents with docker isolation cannot start; process isolation runs commands inside the Weft server.':checks?.sandbox??readiness.error??'Waiting for check',action:'Check the dedicated sandbox daemon configuration and connectivity.'},
 ];
 const connected=services.filter(item=>item.ok).length;const hasResult=!!health.data||!!health.error||!!checks||!!readiness.error;
 return <section className="integration-section admin-section" aria-label="System health"><AdminHeader title="System health" description="Check the services this installation needs to run. These checks do not invoke model providers."><Btn disabled={loading} onClick={()=>void Promise.all([health.reload(),readiness.reload()])}>{loading?'Checking…':'Check services'}</Btn></AdminHeader>
 <div className="card admin-card admin-health-summary" aria-live="polite"><strong>{loading?'Checking four services…':`${connected} of 4 services connected`}</strong>{checkedAt&&<span className="muted small">Last check finished <time dateTime={checkedAt}>{new Date(checkedAt).toLocaleString()}</time>{loading?' · Previous results are being refreshed':''}</span>}</div>
 <section aria-label="Weft services"><h3>Weft services</h3><div className="admin-health-grid" aria-busy={loading}>{services.map(service=><article className="card admin-card" aria-label={service.name} key={service.name}><div className="integration-card-head"><h3>{service.name}</h3><Badge tone={loading?'info':service.ok?'ok':service.warning?'warn':hasResult?'bad':undefined}>{loading?'Checking…':service.ok?'Connected':service.warning?'Check needed':service.unknown?'Could not check':'Unavailable'}</Badge></div><p className="muted small">{service.detail}</p>{!loading&&hasResult&&!service.ok&&<p className="admin-next-action"><strong>Next step</strong><br/>{service.action}</p>}</article>)}</div></section>
 {checks&&(checks.storage!=='ok'||checks.queue!=='ok')&&<p className="integration-note tone-note">One or more dependencies did not pass the readiness check.</p>}
 {readiness.error&&<p role="alert" className="integration-note tone-note">Readiness endpoint failed: {readiness.error}. Previous readiness results are unavailable; retry the check.</p>}
 </section>;
}
