import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const currentDir = dirname(fileURLToPath(import.meta.url))
const routerSource = readFileSync(resolve(currentDir, 'index.js'), 'utf8')
const userStoreSource = readFileSync(resolve(currentDir, '../stores/user.js'), 'utf8')
const forcePasswordChangeSource = readFileSync(resolve(currentDir, '../views/auth/ForcePasswordChange.vue'), 'utf8')

test('stores and guards forced password change state', () => {
  assert.match(userStoreSource, /const mustChangePassword = computed\(\(\) => Boolean\(userInfo\.value\?\.must_change_password\)\)/)
  assert.match(userStoreSource, /mustChangePassword,/)
  assert.match(routerSource, /path: 'force-password-change'/)
  assert.match(routerSource, /name: 'ForcePasswordChange'/)
  assert.match(routerSource, /component: \(\) => import\('@\/views\/auth\/ForcePasswordChange\.vue'\)/)
  assert.match(routerSource, /userStore\.mustChangePassword && to\.name !== 'ForcePasswordChange'/)
  assert.match(routerSource, /next\(\{ name: 'ForcePasswordChange', query: \{ redirect: to\.fullPath \} \}\)/)
})

test('force password change page updates password and refreshes user info', () => {
  assert.match(forcePasswordChangeSource, /import \{ updatePassword \} from '@\/api\/user'/)
  assert.match(forcePasswordChangeSource, /await updatePassword\(\{/)
  assert.match(forcePasswordChangeSource, /await userStore\.getUserInfoAction\(\)/)
  assert.match(forcePasswordChangeSource, /router\.replace\(redirectPath\.value\)/)
  assert.match(forcePasswordChangeSource, /const passwordRules = computed\(\(\) => \[/)
})
