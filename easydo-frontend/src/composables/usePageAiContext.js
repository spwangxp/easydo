import { computed } from 'vue'
import { useRoute } from 'vue-router'

function routeObjectType(name, path) {
  if (name === 'PipelineDetail' || path.startsWith('/pipeline/')) return 'pipeline'
  if (name === 'ResourceK8sBrowser' || path.startsWith('/resources/')) return 'resource'
  if (path.startsWith('/project/')) return 'project'
  if (path.startsWith('/deploy')) return 'deployment'
  if (path.startsWith('/credentials')) return 'credential'
  return 'page'
}

export function usePageAiContext() {
  const route = useRoute()

  const contextRef = computed(() => {
    const objectType = routeObjectType(route.name, route.path)
    const rawID = route.params?.id
    const objectID = Array.isArray(rawID) ? rawID[0] : rawID
    return {
      kind: 'current-page',
      route_path: route.fullPath,
      route_name: String(route.name || ''),
      object_type: objectType,
      object_id: objectID == null ? '' : String(objectID),
      selected_tab: typeof route.query?.tab === 'string' ? route.query.tab : ''
    }
  })

  return {
    contextRef
  }
}
