#!/bin/sh
# Render free-plan entrypoint: migrate first, then run the worker and the
# API in one container (free plans have no pre-deploy hook and no
# background workers). Used as the Docker Command on manual installs:
#   /app/start-render.sh
set -eu
/app/satlight-migrate -dir=up
/app/satlight-worker &
exec /app/satlight-api
