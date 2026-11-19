export const modelSections = ['providers', 'plugins', 'usage', 'budgets', 'prices'] as const;
export type ModelSection = typeof modelSections[number];
export type ProviderEditorRoute = {mode:'create'} | {mode:'edit';name:string};
export type ModelsRoute = {modelSection?:ModelSection;providerEditor?:ProviderEditorRoute};
export const modelSectionTitles:Record<ModelSection,string> = {
  providers:'Providers', plugins:'Inference plugins', usage:'Usage & costs', budgets:'Budgets', prices:'Model prices',
};
export function parseModelsRoute(parts:string[]):ModelsRoute {
  if(parts[1]==='providers' && parts[2]==='new') return {providerEditor:{mode:'create'}};
  if(parts[1]==='providers' && parts[2] && parts[3]==='edit') return {providerEditor:{mode:'edit',name:decodeURIComponent(parts[2])}};
  return modelSections.includes(parts[1] as ModelSection) && parts[1]!=='providers' ? {modelSection:parts[1] as ModelSection} : {};
}
export function modelsHash(route:ModelsRoute):string {
  if(route.providerEditor) return route.providerEditor.mode==='create' ? '#/models/providers/new' : `#/models/providers/${encodeURIComponent(route.providerEditor.name)}/edit`;
  return !route.modelSection || route.modelSection==='providers' ? '#/models' : `#/models/${route.modelSection}`;
}
