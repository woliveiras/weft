import type {Info} from '../api/types';
import {CostsSection} from './CostsSection';
import {ProvidersSection} from './ProvidersSection';
import {modelSections,modelSectionTitles,modelsHash,type ModelsRoute} from './modelsNavigation';

export function ModelsPage({info,modelSection='providers',providerEditor}: {info?:Info} & ModelsRoute) {
  const role=info?.identity?.role;
  const canTest=role==='admin'||role==='operator';
  return <div className="page installation-page models-page">
    {!providerEditor && <>
      <header className="page-head"><div><h1>Models</h1><p className="muted">Manage model providers, inspect usage and configure spending limits.</p></div>
      </header>
      <nav className="section-navigation" aria-label="Model settings sections">{modelSections.map(section=><a key={section} href={modelsHash({modelSection:section})} aria-current={modelSection===section?'page':undefined}>{modelSectionTitles[section]}</a>)}</nav>
    </>}
    {providerEditor || modelSection==='providers' || modelSection==='plugins'
      ? <ProvidersSection canEdit={role==='admin'} canTest={canTest} presentation={modelSection==='plugins'?'plugins':'providers'}
          editor={providerEditor} onEditor={editor=>{location.hash=modelsHash({providerEditor:editor});}}/>
      : <CostsSection key={modelSection} canEdit={role==='admin'} section={modelSection}/>}
  </div>;
}
