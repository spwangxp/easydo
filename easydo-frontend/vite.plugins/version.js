import { execSync } from 'child_process'
import fs from 'fs'
import path from 'path'

/**
 * Vite plugin to generate version information at build time
 * This plugin runs git commands to get commit info and creates a version.js file
 */
export function versionPlugin() {
  let commit = 'unknown'
  let shortCommit = 'unknown'
  let date = 'unknown'

  return {
    name: 'vite-plugin-version',
    buildStart() {
      try {
        // Get full commit hash
        commit = execSync('git rev-parse HEAD', { encoding: 'utf-8' }).trim()
        // Get short commit hash (7 chars)
        shortCommit = execSync('git rev-parse --short HEAD', { encoding: 'utf-8' }).trim()
        // Get build date in ISO format
        date = new Date().toISOString()
      } catch (e) {
        // If git commands fail (e.g., in Docker without .git), use environment variables or fallback values
        commit = process.env.GIT_COMMIT || 'unknown'
        shortCommit = process.env.GIT_COMMIT_SHORT || commit.substring(0, 7)
        date = process.env.GIT_DATE || new Date().toISOString()
      }
    },
    writeBundle(options, bundle) {
      // Generate version.js file content
      const versionContent = `// This file is auto-generated at build time
// DO NOT EDIT MANUALLY

window.__VERSION__ = {
  version: '${process.env.npm_package_version || '1.0.0'}',
  commit: '${commit}',
  shortCommit: '${shortCommit}',
  date: '${date}',
  toString: function() {
    return \`EasyDo Frontend v\${this.version} (commit: \${this.shortCommit}, built: \${this.date})\`
  }
}
`
      // Write version.js to the output directory
      const outputDir = options.dir || path.join(process.cwd(), 'dist')
      const versionFilePath = path.join(outputDir, 'assets', 'version.js')

      // Ensure assets directory exists
      const assetsDir = path.join(outputDir, 'assets')
      if (!fs.existsSync(assetsDir)) {
        fs.mkdirSync(assetsDir, { recursive: true })
      }

      fs.writeFileSync(versionFilePath, versionContent)
      console.log(`[vite-plugin-version] Version info written to: ${versionFilePath}`)
    }
  }
}
