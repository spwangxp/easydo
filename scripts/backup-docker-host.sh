#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
shopt -s nullglob

SCRIPT_STATUS="starting"
SCRIPT_START_TIME="$(date '+%F %T')"
EXECUTION_LOG_FILE=""
SUMMARY_FILE=""
VERIFICATION_LOG_FILE=""
SUMMARY_WRITTEN=0
CURRENT_STEP="startup"
ERR_TRAP_ACTIVE=0
container_count=0
running_container_count=0
volume_count=0
network_count=0

append_execution_log() {
  local line=$1
  [[ -n "${EXECUTION_LOG_FILE:-}" ]] || return 0
  printf '%s\n' "$line" >>"$EXECUTION_LOG_FILE"
}

log() {
  local line
  line=$(printf '[%s] %s' "$(date '+%F %T')" "$*")
  append_execution_log "$line"
}

append_verification_log() {
  local line=$1
  [[ -n "${VERIFICATION_LOG_FILE:-}" ]] || return 0
  printf '%s\n' "$line" >>"$VERIFICATION_LOG_FILE"
}

verification_log() {
  local line
  line=$(printf '[%s] %s' "$(date '+%F %T')" "$*")
  append_verification_log "$line"
}

verification_pass() { verification_log "CHECK [PASS] $1"; }
verification_fail() { verification_log "CHECK [FAIL] $1"; }
verification_info() { verification_log "CHECK [INFO] $1"; }

step_start() {
  CURRENT_STEP="$1"
  log "STEP [START] $1"
}

step_success() { log "STEP [OK] $1"; }
step_info() { log "STEP [INFO] $1"; }
step_fail() { log "STEP [FAIL] $1"; }

die() {
  SCRIPT_STATUS="failed"
  log "ERROR: $*"
  exit 1
}

on_err() {
  local exit_code=$?
  local line_no=${BASH_LINENO[0]:-unknown}

  if (( ERR_TRAP_ACTIVE == 1 )) || [[ "$SCRIPT_STATUS" == "completed" ]]; then
    return "$exit_code"
  fi

  ERR_TRAP_ACTIVE=1
  SCRIPT_STATUS="failed"
  step_fail "${CURRENT_STEP:-unknown} (exit=${exit_code}, line=${line_no})"
  ERR_TRAP_ACTIVE=0
  return "$exit_code"
}

trap 'on_err' ERR

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "missing command: $1"; }
require_root() { [[ "${EUID}" -eq 0 ]] || die "please run as root"; }

safe_name() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's#[^a-z0-9._-]+#_#g; s#(^_+|_+$)##g'
}

short_hash() {
  printf '%s' "$1" | sha256sum | awk '{print substr($1,1,16)}'
}

should_skip_bind_source() {
  local path=$1 docker_root=$2

  case "$path" in
    ""|"/"|"/etc/hosts"|"/etc/hostname"|"/etc/resolv.conf"|"/var/run/docker.sock"|"/run/docker.sock"|"/proc"|"/proc/"*|"/sys"|"/sys/"*|"/dev"|"/dev/"*)
      return 0
      ;;
  esac

  case "$path" in
    "$docker_root"/containers/*|"$docker_root"/overlay2/*|"$docker_root"/image/*|"$docker_root"/network/*)
      return 0
      ;;
  esac

  [[ -S "$path" ]] && return 0
  return 1
}

is_valid_json() {
  local file=$1
  [[ -s "$file" ]] && jq empty "$file" >/dev/null 2>&1
}

is_valid_tar() {
  local file=$1
  [[ -s "$file" ]] && tar -tf "$file" >/dev/null 2>&1
}

container_name_by_id() {
  local cid=$1
  docker inspect --format '{{.Name}}' "$cid" | sed 's#^/##'
}

container_dir_by_id() {
  local cid=$1
  local cname
  cname="$(container_name_by_id "$cid")"
  printf '%s/containers/%s__%s' "$staging" "$(safe_name "$cname")" "${cid:0:12}"
}

container_backup_image() {
  local cid=$1
  local cname
  cname="$(container_name_by_id "$cid")"
  printf 'docker_migration_backup/%s:%s' "$(safe_name "$cname")" "${cid:0:12}"
}

array_contains_exact() {
  local needle=$1
  shift || true
  local item
  for item in "$@"; do
    [[ "$item" == "$needle" ]] && return 0
  done
  return 1
}

print_usage() {
  cat <<'EOF'
Usage:
  backup-docker-host.sh [--exclude-container <name>]... [output_dir]

Options:
  --exclude-container <name>   Exclude a container by exact container name. Repeatable.
  -h, --help                   Show this help message.
EOF
}

write_container_metadata() {
  local cid=$1
  local cdir cname original_image was_running backup_image tmp_meta

  cname="$(container_name_by_id "$cid")"
  cdir="$(container_dir_by_id "$cid")"
  backup_image="$(container_backup_image "$cid")"
  mkdir -p "$cdir"

  docker inspect "$cid" >"$cdir/inspect.json" 2>>"$EXECUTION_LOG_FILE"
  original_image="$(jq -r '.[0].Config.Image' "$cdir/inspect.json")"
  was_running=false
  [[ -n "${running_before_map[$cid]:-}" ]] && was_running=true

  jq -n \
    --arg id "$cid" \
    --arg name "$cname" \
    --arg original_image "$original_image" \
    --arg backup_image "$backup_image" \
    --argjson running_before "$was_running" \
    '{id:$id,name:$name,original_image:$original_image,backup_image:$backup_image,running_before:$running_before}' >"$cdir/meta.json"

  if [[ -f "$cdir/meta.json" ]]; then
    tmp_meta="$(mktemp)"
    jq --arg backup_image "$backup_image" '.backup_image = $backup_image' "$cdir/meta.json" >"$tmp_meta"
    mv "$tmp_meta" "$cdir/meta.json"
  fi
}

export_container_image() {
  local cid=$1
  local cdir backup_image

  cdir="$(container_dir_by_id "$cid")"
  backup_image="$(container_backup_image "$cid")"
  mkdir -p "$cdir"

  docker commit --pause=false "$cid" "$backup_image" >/dev/null 2>>"$EXECUTION_LOG_FILE"
  docker image save -o "$cdir/image.tar" "$backup_image" >>"$EXECUTION_LOG_FILE" 2>&1

  if [[ -f "$cdir/meta.json" ]]; then
    local tmp_meta
    tmp_meta="$(mktemp)"
    jq --arg backup_image "$backup_image" '.backup_image = $backup_image' "$cdir/meta.json" >"$tmp_meta"
    mv "$tmp_meta" "$cdir/meta.json"
  fi
}

write_volume_metadata() {
  local vol=$1
  docker volume inspect "$vol" >"$staging/volumes/meta/$(safe_name "$vol").json" 2>>"$EXECUTION_LOG_FILE"
}

export_volume_archive() {
  local vol=$1
  local meta_file archive_file mountpoint

  meta_file="$staging/volumes/meta/$(safe_name "$vol").json"
  archive_file="$staging/volumes/data/$(safe_name "$vol").tar"

  if ! is_valid_json "$meta_file"; then
    write_volume_metadata "$vol"
  fi

  mountpoint="$(jq -r '.[0].Mountpoint // empty' "$meta_file")"
  if [[ -z "$mountpoint" || ! -d "$mountpoint" ]]; then
    log "WARN: volume mountpoint not accessible, metadata only: $vol"
    return 0
  fi

  tar --numeric-owner --xattrs --acls -cpf "$archive_file" -C "$mountpoint" . >>"$EXECUTION_LOG_FILE" 2>&1
}

rebuild_bind_mount_archives() {
  local tmp_index key src src_type rel archive_rel archive_abs cdir
  local -A bind_seen_local=()

  tmp_index="$(mktemp)"
  : >"$tmp_index"

  for cid in "${all_containers[@]}"; do
    cdir="$(container_dir_by_id "$cid")"
    if ! is_valid_json "$cdir/inspect.json"; then
      write_container_metadata "$cid"
    fi

    while IFS= read -r src; do
      [[ -n "$src" ]] || continue
      if should_skip_bind_source "$src" "$docker_root"; then
        continue
      fi
      if [[ ! -e "$src" ]]; then
        log "WARN: bind source missing, skipped: $src"
        continue
      fi

      key="$(short_hash "$src")"
      [[ -n "${bind_seen_local[$key]:-}" ]] && continue
      bind_seen_local["$key"]=1

      if [[ -d "$src" ]]; then
        src_type="dir"
      else
        src_type="file"
      fi

      rel="${src#/}"
      archive_rel="binds/data/${key}.tar"
      archive_abs="$staging/${archive_rel}"

      if ! is_valid_tar "$archive_abs"; then
        log "bind: $src"
        tar --numeric-owner --xattrs --acls -cpf "$archive_abs" -C / "$rel" >>"$EXECUTION_LOG_FILE" 2>&1
      fi

      printf '%s\t%s\t%s\t%s\n' "$key" "$src_type" "$src" "$archive_rel" >>"$tmp_index"
    done < <(jq -r '.[0].Mounts // [] | .[] | select(.Type=="bind") | .Source' "$cdir/inspect.json")
  done

  mv "$tmp_index" "$staging/meta/bind-mounts.tsv"
}

regenerate_manifest() {
  jq -s \
    --arg created_at "$(date '+%Y-%m-%dT%H:%M:%S%z')" \
    --arg hostname "$host_name" \
    --arg docker_root "$docker_root" \
    --argjson container_count "${#all_containers[@]}" \
    --argjson volume_count "${#all_volumes[@]}" \
    --argjson network_count "${#all_networks[@]}" \
    '{
      created_at: $created_at,
      hostname: $hostname,
      docker_root: $docker_root,
      container_count: $container_count,
      volume_count: $volume_count,
      network_count: $network_count,
      containers: .
    }' "$staging"/containers/*/meta.json >"$staging/meta/manifest.json"
}

generate_internal_checksums() {
  local checksum_file file rel checksum

  checksum_file="$staging/meta/checksums.sha256"
  : >"$checksum_file"

  while IFS= read -r -d '' file; do
    rel="${file#$staging/}"
    checksum="$(sha256sum "$file" | awk '{print $1}')"
    printf '%s  %s\n' "$checksum" "$rel" >>"$checksum_file"
  done < <(find "$staging/containers" "$staging/networks" "$staging/volumes" "$staging/meta" -type f ! -name 'checksums.sha256' -print0)
}

repair_backup_state() {
  local cid cdir image_file vol meta_file archive_file net network_file

  log "repairing missing or invalid backup items"

  for cid in "${all_containers[@]}"; do
    cdir="$(container_dir_by_id "$cid")"
    mkdir -p "$cdir"

    if ! is_valid_json "$cdir/inspect.json" || ! is_valid_json "$cdir/meta.json"; then
      write_container_metadata "$cid"
    fi

    image_file="$cdir/image.tar"
    if ! is_valid_tar "$image_file"; then
      export_container_image "$cid"
    fi
  done

  for net in "${all_networks[@]}"; do
    [[ "$net" == "bridge" || "$net" == "host" || "$net" == "none" ]] && continue
    network_file="$staging/networks/$(safe_name "$net").json"
    if ! is_valid_json "$network_file"; then
      docker network inspect "$net" >"$network_file" 2>>"$EXECUTION_LOG_FILE"
    fi
  done

  for vol in "${all_volumes[@]}"; do
    meta_file="$staging/volumes/meta/$(safe_name "$vol").json"
    archive_file="$staging/volumes/data/$(safe_name "$vol").tar"

    if ! is_valid_json "$meta_file"; then
      write_volume_metadata "$vol"
    fi

    if [[ -d "$(jq -r '.[0].Mountpoint // empty' "$meta_file")" ]] && ! is_valid_tar "$archive_file"; then
      export_volume_archive "$vol"
    fi
  done

  rebuild_bind_mount_archives
  regenerate_manifest
  generate_internal_checksums
}

verify_backup_state() {
  local cid cdir image_file vol meta_file archive_file net network_file
  local report_file issues expected_count actual_count archive_rel archive_abs

  report_file="$staging/meta/verification-report.txt"
  : >"$report_file"
  : >"$VERIFICATION_LOG_FILE"
  issues=0

  verification_info "starting backup verification"
  verification_info "expected containers=${#all_containers[@]}, volumes=${#all_volumes[@]}, networks=${#all_networks[@]}"

  for cid in "${all_containers[@]}"; do
    cdir="$(container_dir_by_id "$cid")"
    image_file="$cdir/image.tar"
    verification_info "verifying container $(container_name_by_id "$cid") (${cid:0:12})"

    if ! is_valid_json "$cdir/inspect.json"; then
      printf 'container inspect invalid or missing: %s\n' "$cid" >>"$report_file"
      verification_fail "container inspect invalid or missing: $(container_name_by_id "$cid") (${cid:0:12}) -> $cdir/inspect.json"
      issues=$((issues + 1))
    else
      verification_pass "container inspect valid: $(container_name_by_id "$cid") (${cid:0:12})"
    fi

    if ! is_valid_json "$cdir/meta.json"; then
      printf 'container meta invalid or missing: %s\n' "$cid" >>"$report_file"
      verification_fail "container meta invalid or missing: $(container_name_by_id "$cid") (${cid:0:12}) -> $cdir/meta.json"
      issues=$((issues + 1))
    elif [[ -z "$(jq -r '.backup_image // empty' "$cdir/meta.json")" ]]; then
      printf 'container backup_image missing in meta: %s\n' "$cid" >>"$report_file"
      verification_fail "container backup_image missing in meta: $(container_name_by_id "$cid") (${cid:0:12})"
      issues=$((issues + 1))
    else
      verification_pass "container metadata valid: $(container_name_by_id "$cid") (${cid:0:12})"
    fi

    if ! is_valid_tar "$image_file"; then
      printf 'container image tar invalid or missing: %s\n' "$cid" >>"$report_file"
      verification_fail "container image archive invalid or missing: $(container_name_by_id "$cid") (${cid:0:12}) -> $image_file"
      issues=$((issues + 1))
    else
      verification_pass "container image archive valid: $(container_name_by_id "$cid") (${cid:0:12})"
    fi
  done

  for net in "${all_networks[@]}"; do
    [[ "$net" == "bridge" || "$net" == "host" || "$net" == "none" ]] && continue
    network_file="$staging/networks/$(safe_name "$net").json"
    verification_info "verifying network $net"
    if ! is_valid_json "$network_file"; then
      printf 'network inspect invalid or missing: %s\n' "$net" >>"$report_file"
      verification_fail "network inspect invalid or missing: $net -> $network_file"
      issues=$((issues + 1))
    else
      verification_pass "network inspect valid: $net"
    fi
  done

  for vol in "${all_volumes[@]}"; do
    meta_file="$staging/volumes/meta/$(safe_name "$vol").json"
    archive_file="$staging/volumes/data/$(safe_name "$vol").tar"
    verification_info "verifying volume $vol"

    if ! is_valid_json "$meta_file"; then
      printf 'volume metadata invalid or missing: %s\n' "$vol" >>"$report_file"
      verification_fail "volume metadata invalid or missing: $vol -> $meta_file"
      issues=$((issues + 1))
      continue
    else
      verification_pass "volume metadata valid: $vol"
    fi

    if [[ -d "$(jq -r '.[0].Mountpoint // empty' "$meta_file")" ]] && ! is_valid_tar "$archive_file"; then
      printf 'volume archive invalid or missing: %s\n' "$vol" >>"$report_file"
      verification_fail "volume archive invalid or missing: $vol -> $archive_file"
      issues=$((issues + 1))
    elif [[ -f "$archive_file" ]]; then
      verification_pass "volume archive valid: $vol"
    else
      verification_info "volume archive not present because mountpoint is unavailable or metadata-only: $vol"
    fi
  done

  if [[ ! -f "$staging/meta/bind-mounts.tsv" ]]; then
    printf 'bind mount index missing\n' >>"$report_file"
    verification_fail "bind mount index missing: $staging/meta/bind-mounts.tsv"
    issues=$((issues + 1))
  else
    verification_pass "bind mount index present: $staging/meta/bind-mounts.tsv"
    while IFS=$'\t' read -r _bind_key _bind_type _bind_source archive_rel; do
      [[ -n "$archive_rel" ]] || continue
      archive_abs="$staging/$archive_rel"
      verification_info "verifying bind archive source=${_bind_source:-unknown} archive=$archive_rel"
      if ! is_valid_tar "$archive_abs"; then
        printf 'bind archive invalid or missing: %s\n' "$archive_rel" >>"$report_file"
        verification_fail "bind archive invalid or missing: $archive_abs"
        issues=$((issues + 1))
      else
        verification_pass "bind archive valid: $archive_abs"
      fi
    done <"$staging/meta/bind-mounts.tsv"
  fi

  if ! is_valid_json "$staging/meta/manifest.json"; then
    printf 'manifest invalid or missing\n' >>"$report_file"
    verification_fail "manifest invalid or missing: $staging/meta/manifest.json"
    issues=$((issues + 1))
  else
    expected_count="${#all_containers[@]}"
    actual_count="$(jq -r '.containers | length' "$staging/meta/manifest.json" 2>>"$VERIFICATION_LOG_FILE")"
    if [[ "$actual_count" != "$expected_count" ]]; then
      printf 'manifest container count mismatch: expected=%s actual=%s\n' "$expected_count" "$actual_count" >>"$report_file"
      verification_fail "manifest container count mismatch: expected=$expected_count actual=$actual_count"
      issues=$((issues + 1))
    else
      verification_pass "manifest valid with expected container count: $actual_count"
    fi
  fi

  if [[ ! -s "$staging/meta/checksums.sha256" ]]; then
    printf 'checksums file missing or empty\n' >>"$report_file"
    verification_fail "checksums file missing or empty: $staging/meta/checksums.sha256"
    issues=$((issues + 1))
  else
    verification_pass "checksums file present and non-empty: $staging/meta/checksums.sha256"
  fi

  if (( issues == 0 )); then
    printf 'backup verification passed\n' >"$report_file"
    verification_pass "backup verification passed with zero issues"
    return 0
  fi

  verification_fail "backup verification finished with issues=$issues; see $report_file"
  return 1
}

declare -a exclude_containers=()
OUTPUT_DIR=""

while (($# > 0)); do
  case "$1" in
    --exclude-container)
      shift
      [[ $# -gt 0 ]] || die "--exclude-container requires a container name"
      exclude_containers+=("$1")
      shift
      ;;
    -h|--help)
      print_usage
      exit 0
      ;;
    *)
      if [[ -z "$OUTPUT_DIR" ]]; then
        OUTPUT_DIR="$1"
        shift
      else
        die "unexpected argument: $1"
      fi
      ;;
  esac
done

require_root
need_cmd docker
need_cmd jq
need_cmd tar
need_cmd gzip
need_cmd sha256sum
need_cmd hostname
need_cmd mktemp

OUTPUT_DIR="${OUTPUT_DIR:-$PWD}"
MAX_RETRIES="${MAX_RETRIES:-0}"
mkdir -p "$OUTPUT_DIR"

timestamp="$(date '+%Y%m%d-%H%M%S')"
host_name="$(hostname -s 2>/dev/null || hostname)"
backup_name="docker-host-backup-${host_name}-${timestamp}"
staging="${OUTPUT_DIR}/${backup_name}"
archive="${OUTPUT_DIR}/${backup_name}.tar.gz"
docker_root="$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || echo /var/lib/docker)"
EXECUTION_LOG_FILE="${OUTPUT_DIR}/${backup_name}.execution.log"
SUMMARY_FILE="${OUTPUT_DIR}/${backup_name}.summary.log"
VERIFICATION_LOG_FILE="${OUTPUT_DIR}/${backup_name}.verification.log"

: >"$EXECUTION_LOG_FILE"
log "execution log: $EXECUTION_LOG_FILE"
log "summary log: $SUMMARY_FILE"
log "verification log: $VERIFICATION_LOG_FILE"

mkdir -p \
  "$staging/meta" \
  "$staging/containers" \
  "$staging/networks" \
  "$staging/volumes/meta" \
  "$staging/volumes/data" \
  "$staging/binds/data"

docker version >"$staging/meta/docker-version.txt" 2>>"$EXECUTION_LOG_FILE"
docker info >"$staging/meta/docker-info.txt" 2>>"$EXECUTION_LOG_FILE"
docker system df >"$staging/meta/docker-system-df.txt" 2>>"$EXECUTION_LOG_FILE"
printf '%s\n' "$docker_root" >"$staging/meta/docker-root-dir.txt"

mapfile -t discovered_containers < <(docker ps -aq --no-trunc)
mapfile -t discovered_running_containers < <(docker ps -q --no-trunc)

all_containers=()
for cid in "${discovered_containers[@]}"; do
  cname="$(container_name_by_id "$cid")"
  if array_contains_exact "$cname" "${exclude_containers[@]}"; then
    log "excluding container: $cname"
    continue
  fi
  all_containers+=("$cid")
done

((${#all_containers[@]} > 0)) || die "no containers found"

running_containers=()
for cid in "${discovered_running_containers[@]}"; do
  cname="$(container_name_by_id "$cid")"
  if array_contains_exact "$cname" "${exclude_containers[@]}"; then
    continue
  fi
  running_containers+=("$cid")
done

declare -A volume_seen=()
declare -A network_seen=()
all_volumes=()
all_networks=()

for cid in "${all_containers[@]}"; do
  while IFS= read -r vol_name; do
    [[ -n "$vol_name" ]] || continue
    if [[ -z "${volume_seen[$vol_name]:-}" ]]; then
      volume_seen["$vol_name"]=1
      all_volumes+=("$vol_name")
    fi
  done < <(docker inspect "$cid" | jq -r '.[0].Mounts // [] | .[] | select(.Type=="volume") | .Name // empty')

  while IFS= read -r net_name; do
    [[ -n "$net_name" ]] || continue
    if [[ -z "${network_seen[$net_name]:-}" ]]; then
      network_seen["$net_name"]=1
      all_networks+=("$net_name")
    fi
  done < <(docker inspect "$cid" | jq -r '.[0].NetworkSettings.Networks | keys[]?')
done

printf '%s\n' "${all_containers[@]}" >"$staging/meta/all-containers.txt"
printf '%s\n' "${running_containers[@]}" >"$staging/meta/running-containers-before-backup.txt"
printf '%s\n' "${all_volumes[@]}" >"$staging/meta/all-volumes.txt"
printf '%s\n' "${all_networks[@]}" >"$staging/meta/all-networks.txt"
printf '%s\n' "${exclude_containers[@]}" >"$staging/meta/excluded-containers.txt"

container_count=${#all_containers[@]}
running_container_count=${#running_containers[@]}
volume_count=${#all_volumes[@]}
network_count=${#all_networks[@]}
step_info "discovered containers=${container_count}, running=${running_container_count}, volumes=${volume_count}, networks=${network_count}"

declare -A running_before_map=()
for cid in "${running_containers[@]}"; do
  running_before_map["$cid"]=1
done

write_summary() {
  local finished_at

  [[ -n "${SUMMARY_FILE:-}" ]] || return 0
  (( SUMMARY_WRITTEN == 0 )) || return 0

  finished_at="$(date '+%F %T')"
  {
    printf 'status=%s\n' "$SCRIPT_STATUS"
    printf 'started_at=%s\n' "$SCRIPT_START_TIME"
    printf 'finished_at=%s\n' "$finished_at"
    printf 'current_step=%s\n' "${CURRENT_STEP:-unknown}"
    printf 'archive=%s\n' "${archive:-}"
    printf 'staging=%s\n' "${staging:-}"
    printf 'execution_log=%s\n' "${EXECUTION_LOG_FILE:-}"
    printf 'verification_log=%s\n' "${VERIFICATION_LOG_FILE:-}"
    printf 'containers=%s\n' "${container_count:-0}"
    printf 'running_containers=%s\n' "${running_container_count:-0}"
    printf 'volumes=%s\n' "${volume_count:-0}"
    printf 'networks=%s\n' "${network_count:-0}"
  } >"$SUMMARY_FILE"

  SUMMARY_WRITTEN=1
  log "summary written: $SUMMARY_FILE"
}

containers_restarted=0
cleanup() {
  if (( containers_restarted == 0 )) && ((${#running_containers[@]} > 0)); then
    log "attempting to restart containers that were running before backup"
    docker start "${running_containers[@]}" >/dev/null 2>&1 || true
  fi

  if [[ "$SCRIPT_STATUS" != "completed" ]]; then
    step_fail "backup exited before completion"
  fi

  write_summary
  return 0
}
trap cleanup EXIT

step_start "saving network metadata"
for net in "${all_networks[@]}"; do
  [[ "$net" == "bridge" || "$net" == "host" || "$net" == "none" ]] && continue
  step_info "saving network metadata: $net"
  docker network inspect "$net" >"$staging/networks/$(safe_name "$net").json" 2>>"$EXECUTION_LOG_FILE"
done
step_success "saved network metadata"

step_start "saving volume metadata"
for vol in "${all_volumes[@]}"; do
  step_info "saving volume metadata: $vol"
  docker volume inspect "$vol" >"$staging/volumes/meta/$(safe_name "$vol").json" 2>>"$EXECUTION_LOG_FILE"
done
step_success "saved volume metadata"

step_start "saving container inspect metadata"
for cid in "${all_containers[@]}"; do
  step_info "saving container inspect metadata: $(container_name_by_id "$cid") (${cid:0:12})"
  write_container_metadata "$cid"
done
step_success "saved container inspect metadata"

if ((${#running_containers[@]} > 0)); then
  step_start "stopping running containers for consistent backup"
  docker stop --time 30 "${running_containers[@]}" >/dev/null 2>>"$EXECUTION_LOG_FILE"
  step_success "stopped running containers"
else
  step_info "no running containers required stopping"
fi

step_start "committing container writable layers and exporting images"
for cid in "${all_containers[@]}"; do
  step_info "exporting container image: $(container_name_by_id "$cid") (${cid:0:12})"
  export_container_image "$cid"
done
step_success "exported container images"

step_start "backing up named volumes"
for vol in "${all_volumes[@]}"; do
  step_info "backing up volume: $vol"
  export_volume_archive "$vol"
done
step_success "backed up named volumes"

step_start "backing up unique bind mounts"
rebuild_bind_mount_archives
step_success "backed up unique bind mounts"

step_start "creating manifest"
regenerate_manifest
step_success "created manifest"

step_start "generating checksums"
generate_internal_checksums
step_success "generated checksums"

attempt=1
step_start "verifying backup completeness"
while true; do
  step_info "verification attempt ${attempt}"
  if verify_backup_state; then
    break
  fi

  if [[ "$MAX_RETRIES" != "0" && "$attempt" -ge "$MAX_RETRIES" ]]; then
    log "verification report:"
    verification_log "[FINAL] verification failed after attempt ${attempt}; report_file=$staging/meta/verification-report.txt"
    die "backup verification did not pass within MAX_RETRIES=${MAX_RETRIES}"
  fi

  step_info "verification failed, retrying missing items"
  verification_log "[RETRY] retrying backup repair after failed verification attempt ${attempt}; report_file=$staging/meta/verification-report.txt"
  repair_backup_state
  attempt=$((attempt + 1))
done
step_success "verified backup completeness"

if ((${#running_containers[@]} > 0)); then
  step_start "restarting containers that were running before backup"
  docker start "${running_containers[@]}" >/dev/null 2>>"$EXECUTION_LOG_FILE"
  step_success "restarted containers that were running before backup"
fi
containers_restarted=1

step_start "creating final archive"
tar -czpf "$archive" -C "$OUTPUT_DIR" "$backup_name" >>"$EXECUTION_LOG_FILE" 2>&1
tar -tzf "$archive" >/dev/null 2>>"$EXECUTION_LOG_FILE"
sha256sum "$archive" >"${archive}.sha256"
step_success "created final archive"

if [[ "${KEEP_WORKDIR:-0}" != "1" ]]; then
  step_info "removing staging directory: $staging"
  rm -rf "$staging"
fi

SCRIPT_STATUS="completed"
write_summary
log "backup completed: $archive"
log "archive checksum: ${archive}.sha256"
