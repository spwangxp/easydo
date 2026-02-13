#!/bin/sh

# Print EasyDo Frontend version information (single line for container logs)
echo ""
echo "[INFO] EasyDo Frontend v1.0.0 (commit: ${GIT_COMMIT_SHORT:-unknown}, built: ${GIT_DATE:-unknown})"
echo ""

# Check if version.js exists
if [ -f "/usr/share/nginx/html/assets/version.js" ]; then
  echo "[INFO] Version file found: /usr/share/nginx/html/assets/version.js"
fi
