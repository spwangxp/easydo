#!/bin/sh

# EasyDo Frontend Startup Script
# Prints version information on container start

echo "=========================================="
echo "[INFO] EasyDo Frontend v1.0.0 (commit: ${GIT_COMMIT_SHORT:-unknown}, built: ${GIT_DATE:-unknown})"
echo "[INFO] EasyDo Frontend Version Details:"
echo "  Version:   1.0.0"
if [ -n "$GIT_COMMIT" ]; then
  echo "  Commit:    $GIT_COMMIT"
  echo "  Short:     $GIT_COMMIT_SHORT"
else
  echo "  Commit:    unknown"
  echo "  Short:     unknown"
fi
if [ -n "$GIT_DATE" ]; then
  echo "  Built:     $GIT_DATE"
else
  echo "  Built:     unknown"
fi
echo "=========================================="
echo ""

# Check if version.js exists
if [ -f "/usr/share/nginx/html/assets/version.js" ]; then
  echo "[INFO] Version file found: /usr/share/nginx/html/assets/version.js"
fi

# Start nginx
exec nginx -g "daemon off;"
