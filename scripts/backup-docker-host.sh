#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
shopt -s nullglob

log() { printf '[%s] %s\n' "$(date '+%F %T')" "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }
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

  docker inspect "$cid" >"$cdir/inspect.json"
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

  docker commit --pause=false "$cid" "$backup_image" >/dev/null
  docker image save -o "$cdir/image.tar" "$backup_image"

  if [[ -f "$cdir/meta.json" ]]; then
    local tmp_meta
    tmp_meta="$(mktemp)"
    jq --arg backup_image "$backup_image" '.backup_image = $backup_image' "$cdir/meta.json" >"$tmp_meta"
    mv "$tmp_meta" "$cdir/meta.json"
  fi
}

write_volume_metadata() {
  local vol=$1
  docker volume inspect "$vol" >"$staging/volumes/meta/$(safe_name "$vol").json"
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

  tar --numeric-owner --xattrs --acls -cpf "$archive_file" -C "$mountpoint" .
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
        tar --numeric-owner --xattrs --acls -cpf "$archive_abs" -C / "$rel"
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
      docker network inspect "$net" >"$network_file"
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
  issues=0

  for cid in "${all_containers[@]}"; do
    cdir="$(container_dir_by_id "$cid")"
    image_file="$cdir/image.tar"

    if ! is_valid_json "$cdir/inspect.json"; then
      printf 'container inspect invalid or missing: %s\n' "$cid" >>"$report_file"
      issues=$((issues + 1))
    fi

    if ! is_valid_json "$cdir/meta.json"; then
      printf 'container meta invalid or missing: %s\n' "$cid" >>"$report_file"
      issues=$((issues + 1))
    elif [[ -z "$(jq -r '.backup_image // empty' "$cdir/meta.json")" ]]; then
      printf 'container backup_image missing in meta: %s\n' "$cid" >>"$report_file"
      issues=$((issues + 1))
    fi

    if ! is_valid_tar "$image_file"; then
      printf 'container image tar invalid or missing: %s\n' "$cid" >>"$report_file"
      issues=$((issues + 1))
    fi
  done

  for net in "${all_networks[@]}"; do
    [[ "$net" == "bridge" || "$net" == "host" || "$net" == "none" ]] && continue
    network_file="$staging/networks/$(safe_name "$net").json"
    if ! is_valid_json "$network_file"; then
      printf 'network inspect invalid or missing: %s\n' "$net" >>"$report_file"
      issues=$((issues + 1))
    fi
  done

  for vol in "${all_volumes[@]}"; do
    meta_file="$staging/volumes/meta/$(safe_name "$vol").json"
    archive_file="$staging/volumes/data/$(safe_name "$vol").tar"

    if ! is_valid_json "$meta_file"; then
      printf 'volume metadata invalid or missing: %s\n' "$vol" >>"$report_file"
      issues=$((issues + 1))
      continue
    fi

    if [[ -d "$(jq -r '.[0].Mountpoint // empty' "$meta_file")" ]] && ! is_valid_tar "$archive_file"; then
      printf 'volume archive invalid or missing: %s\n' "$vol" >>"$report_file"
      issues=$((issues + 1))
    fi
  done

  if [[ ! -f "$staging/meta/bind-mounts.tsv" ]]; then
    printf 'bind mount index missing\n' >>"$report_file"
    issues=$((issues + 1))
  else
    while IFS=$'\t' read -r _bind_key _bind_type _bind_source archive_rel; do
      [[ -n "$archive_rel" ]] || continue
      archive_abs="$staging/$archive_rel"
      if ! is_valid_tar "$archive_abs"; then
        printf 'bind archive invalid or missing: %s\n' "$archive_rel" >>"$report_file"
        issues=$((issues + 1))
      fi
    done <"$staging/meta/bind-mounts.tsv"
  fi

  if ! is_valid_json "$staging/meta/manifest.json"; then
    printf 'manifest invalid or missing\n' >>"$report_file"
    issues=$((issues + 1))
  else
    expected_count="${#all_containers[@]}"
    actual_count="$(jq -r '.containers | length' "$staging/meta/manifest.json")"
    if [[ "$actual_count" != "$expected_count" ]]; then
      printf 'manifest container count mismatch: expected=%s actual=%s\n' "$expected_count" "$actual_count" >>"$report_file"
      issues=$((issues + 1))
    fi
  fi

  if [[ ! -s "$staging/meta/checksums.sha256" ]]; then
    printf 'checksums file missing or empty\n' >>"$report_file"
    issues=$((issues + 1))
  fi

  if (( issues == 0 )); then
    printf 'backup verification passed\n' >"$report_file"
    return 0
  fi

  return 1
}

require_root
need_cmd docker
need_cmd jq
need_cmd tar
need_cmd gzip
need_cmd sha256sum
need_cmd hostname
need_cmd mktemp

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

OUTPUT_DIR="${OUTPUT_DIR:-$PWD}"
MAX_RETRIES="${MAX_RETRIES:-0}"
mkdir -p "$OUTPUT_DIR"

timestamp="$(date '+%Y%m%d-%H%M%S')"
host_name="$(hostname -s 2>/dev/null || hostname)"
backup_name="docker-host-backup-${host_name}-${timestamp}"
staging="${OUTPUT_DIR}/${backup_name}"
archive="${OUTPUT_DIR}/${backup_name}.tar.gz"
docker_root="$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || echo /var/lib/docker)"

mkdir -p \
  "$staging/meta" \
  "$staging/containers" \
  "$staging/networks" \
  "$staging/volumes/meta" \
  "$staging/volumes/data" \
  "$staging/binds/data"

docker version >"$staging/meta/docker-version.txt"
docker info >"$staging/meta/docker-info.txt"
docker system df >"$staging/meta/docker-system-df.txt"
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

declare -A running_before_map=()
for cid in "${running_containers[@]}"; do
  running_before_map["$cid"]=1
done

containers_restarted=0
cleanup() {
  if (( containers_restarted == 0 )) && ((${#running_containers[@]} > 0)); then
    log "attempting to restart containers that were running before backup"
    docker start "${running_containers[@]}" >/dev/null 2>&1 || true
  fi
  return 0
}
trap cleanup EXIT

log "saving network metadata"
for net in "${all_networks[@]}"; do
  [[ "$net" == "bridge" || "$net" == "host" || "$net" == "none" ]] && continue
  docker network inspect "$net" >"$staging/networks/$(safe_name "$net").json"
done

log "saving volume metadata"
for vol in "${all_volumes[@]}"; do
  docker volume inspect "$vol" >"$staging/volumes/meta/$(safe_name "$vol").json"
done

log "saving container inspect metadata"
for cid in "${all_containers[@]}"; do
  write_container_metadata "$cid"
done

if ((${#running_containers[@]} > 0)); then
  log "stopping running containers for consistent backup"
  docker stop --time 30 "${running_containers[@]}" >/dev/null
fi

log "committing container writable layers and exporting images"
for cid in "${all_containers[@]}"; do
  export_container_image "$cid"
done

log "backing up named volumes"
for vol in "${all_volumes[@]}"; do
  log "volume: $vol"
  export_volume_archive "$vol"
done

log "backing up unique bind mounts"
rebuild_bind_mount_archives

log "creating manifest"
regenerate_manifest

log "generating checksums"
generate_internal_checksums

attempt=1
while true; do
  log "verifying backup completeness (attempt ${attempt})"
  if verify_backup_state; then
    break
  fi

  if [[ "$MAX_RETRIES" != "0" && "$attempt" -ge "$MAX_RETRIES" ]]; then
    log "verification report:"
    cat "$staging/meta/verification-report.txt" >&2
    die "backup verification did not pass within MAX_RETRIES=${MAX_RETRIES}"
  fi

  log "verification failed, retrying missing items"
  cat "$staging/meta/verification-report.txt" >&2
  repair_backup_state
  attempt=$((attempt + 1))
done

if ((${#running_containers[@]} > 0)); then
  log "restarting containers that were running before backup"
  docker start "${running_containers[@]}" >/dev/null
fi
containers_restarted=1

log "creating final archive"
tar -czpf "$archive" -C "$OUTPUT_DIR" "$backup_name"
tar -tzf "$archive" >/dev/null
sha256sum "$archive" >"${archive}.sha256"

if [[ "${KEEP_WORKDIR:-0}" != "1" ]]; then
  rm -rf "$staging"
fi

log "backup completed: $archive"
log "archive checksum: ${archive}.sha256"
