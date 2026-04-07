#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
shopt -s nullglob

SCRIPT_STATUS="starting"
SCRIPT_START_TIME="$(date '+%F %T')"
EXECUTION_LOG_FILE=""
SUMMARY_FILE=""
VALIDATION_LOG_FILE=""
SUMMARY_WRITTEN=0
CURRENT_STEP="startup"
ERR_TRAP_ACTIVE=0
preflight_report=""
container_count=0
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

append_validation_log() {
  local line=$1
  [[ -n "${VALIDATION_LOG_FILE:-}" ]] || return 0
  printf '%s\n' "$line" >>"$VALIDATION_LOG_FILE"
}

validation_log() {
  local line
  line=$(printf '[%s] %s' "$(date '+%F %T')" "$*")
  append_validation_log "$line"
}

validation_pass() { validation_log "CHECK [PASS] $1"; }
validation_fail() { validation_log "CHECK [FAIL] $1"; }
validation_info() { validation_log "CHECK [INFO] $1"; }

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
require_root() {
  [[ "${EASYDO_SKIP_ROOT_CHECK:-0}" == "1" ]] && return 0
  [[ "${EUID}" -eq 0 ]] || die "please run as root"
}

safe_name() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's#[^a-z0-9._-]+#_#g; s#(^_+|_+$)##g'
}

is_valid_json() {
  local file=$1
  [[ -s "$file" ]] && jq empty "$file" >/dev/null 2>&1
}

is_valid_tar() {
  local file=$1
  [[ -s "$file" ]] && tar -tf "$file" >/dev/null 2>&1
}

append_scalar_flag() {
  local -n cmd_ref=$1
  local value=$2 flag=$3
  [[ -n "$value" && "$value" != "null" ]] && cmd_ref+=("$flag" "$value")
}

create_network_from_inspect() {
  local json=$1
  local name driver

  name="$(jq -r '.[0].Name' "$json")"
  [[ "$name" == "bridge" || "$name" == "host" || "$name" == "none" ]] && return 0

  if docker network inspect "$name" >/dev/null 2>&1; then
    log "network exists, skip: $name"
    return 0
  fi

  driver="$(jq -r '.[0].Driver // "bridge"' "$json")"
  cmd=(docker network create --driver "$driver")

  [[ "$(jq -r '.[0].Internal // false' "$json")" == "true" ]] && cmd+=(--internal)
  [[ "$(jq -r '.[0].EnableIPv6 // false' "$json")" == "true" ]] && cmd+=(--ipv6)
  [[ "$(jq -r '.[0].Attachable // false' "$json")" == "true" ]] && cmd+=(--attachable)

  while IFS=$'\t' read -r subnet gateway iprange; do
    [[ -n "$subnet" ]] && cmd+=(--subnet "$subnet")
    [[ -n "$gateway" ]] && cmd+=(--gateway "$gateway")
    [[ -n "$iprange" ]] && cmd+=(--ip-range "$iprange")
  done < <(jq -r '.[0].IPAM.Config // [] | .[] | [(.Subnet // ""), (.Gateway // ""), (.IPRange // "")] | @tsv' "$json")

  while IFS=$'\t' read -r key value; do
    [[ -n "$key" ]] && cmd+=(--opt "${key}=${value}")
  done < <(jq -r '.[0].Options // {} | to_entries[] | [.key, (.value|tostring)] | @tsv' "$json")

  while IFS=$'\t' read -r key value; do
    [[ -n "$key" ]] && cmd+=(--label "${key}=${value}")
  done < <(jq -r '.[0].Labels // {} | to_entries[] | [.key, (.value|tostring)] | @tsv' "$json")

  cmd+=("$name")
  "${cmd[@]}" >>"$EXECUTION_LOG_FILE" 2>&1
}

create_volume_from_inspect() {
  local json=$1
  local name driver

  name="$(jq -r '.[0].Name' "$json")"
  if docker volume inspect "$name" >/dev/null 2>&1; then
    log "volume exists, skip create: $name"
    return 0
  fi

  driver="$(jq -r '.[0].Driver // "local"' "$json")"
  cmd=(docker volume create --driver "$driver")

  while IFS=$'\t' read -r key value; do
    [[ -n "$key" ]] && cmd+=(--opt "${key}=${value}")
  done < <(jq -r '.[0].Options // {} | to_entries[] | [.key, (.value|tostring)] | @tsv' "$json")

  while IFS=$'\t' read -r key value; do
    [[ -n "$key" ]] && cmd+=(--label "${key}=${value}")
  done < <(jq -r '.[0].Labels // {} | to_entries[] | [.key, (.value|tostring)] | @tsv' "$json")

  cmd+=("$name")
  "${cmd[@]}" >>"$EXECUTION_LOG_FILE" 2>&1
}

create_container_from_inspect() {
  local inspect=$1 backup_image=$2 name=$3
  local network_mode restart_name restart_max
  local create_stderr_file=${4:-}

  cmd=(docker create --name "$name")

  append_scalar_flag cmd "$(jq -r '.[0].Config.Hostname // empty' "$inspect")" --hostname
  append_scalar_flag cmd "$(jq -r '.[0].Config.Domainname // empty' "$inspect")" --domainname
  append_scalar_flag cmd "$(jq -r '.[0].Config.User // empty' "$inspect")" --user
  append_scalar_flag cmd "$(jq -r '.[0].Config.WorkingDir // empty' "$inspect")" --workdir
  append_scalar_flag cmd "$(jq -r '.[0].Config.StopSignal // empty' "$inspect")" --stop-signal

  stop_timeout="$(jq -r '.[0].Config.StopTimeout // empty' "$inspect")"
  [[ -n "$stop_timeout" && "$stop_timeout" != "null" ]] && cmd+=(--stop-timeout "$stop_timeout")

  runtime="$(jq -r '.[0].HostConfig.Runtime // empty' "$inspect")"
  [[ -n "$runtime" && "$runtime" != "runc" ]] && cmd+=(--runtime "$runtime")

  memory="$(jq -r '.[0].HostConfig.Memory // 0' "$inspect")"
  (( memory > 0 )) && cmd+=(--memory "$memory")

  memory_reservation="$(jq -r '.[0].HostConfig.MemoryReservation // 0' "$inspect")"
  (( memory_reservation > 0 )) && cmd+=(--memory-reservation "$memory_reservation")

  memory_swap="$(jq -r '.[0].HostConfig.MemorySwap // 0' "$inspect")"
  (( memory_swap > 0 )) && cmd+=(--memory-swap "$memory_swap")

  nano_cpus="$(jq -r '.[0].HostConfig.NanoCpus // 0' "$inspect")"
  if (( nano_cpus > 0 )); then
    cpus="$(awk "BEGIN { printf \"%.3f\", ${nano_cpus}/1000000000 }")"
    cmd+=(--cpus "$cpus")
  fi

  cpu_shares="$(jq -r '.[0].HostConfig.CpuShares // 0' "$inspect")"
  (( cpu_shares > 0 )) && cmd+=(--cpu-shares "$cpu_shares")

  append_scalar_flag cmd "$(jq -r '.[0].HostConfig.CpusetCpus // empty' "$inspect")" --cpuset-cpus
  append_scalar_flag cmd "$(jq -r '.[0].HostConfig.CpusetMems // empty' "$inspect")" --cpuset-mems
  append_scalar_flag cmd "$(jq -r '.[0].HostConfig.PidMode // empty' "$inspect")" --pid
  append_scalar_flag cmd "$(jq -r '.[0].HostConfig.IpcMode // empty' "$inspect")" --ipc
  append_scalar_flag cmd "$(jq -r '.[0].HostConfig.UTSMode // empty' "$inspect")" --uts
  append_scalar_flag cmd "$(jq -r '.[0].HostConfig.UsernsMode // empty' "$inspect")" --userns
  append_scalar_flag cmd "$(jq -r '.[0].HostConfig.CgroupnsMode // empty' "$inspect")" --cgroupns
  append_scalar_flag cmd "$(jq -r '.[0].HostConfig.CgroupParent // empty' "$inspect")" --cgroup-parent

  shm_size="$(jq -r '.[0].HostConfig.ShmSize // 0' "$inspect")"
  (( shm_size > 0 )) && cmd+=(--shm-size "$shm_size")

  pids_limit="$(jq -r '.[0].HostConfig.PidsLimit // -1' "$inspect")"
  (( pids_limit >= 0 )) && cmd+=(--pids-limit "$pids_limit")

  oom_score_adj="$(jq -r '.[0].HostConfig.OomScoreAdj // 0' "$inspect")"
  (( oom_score_adj != 0 )) && cmd+=(--oom-score-adj "$oom_score_adj")

  [[ "$(jq -r '.[0].HostConfig.Privileged // false' "$inspect")" == "true" ]] && cmd+=(--privileged)
  [[ "$(jq -r '.[0].HostConfig.ReadonlyRootfs // false' "$inspect")" == "true" ]] && cmd+=(--read-only)
  [[ "$(jq -r '.[0].HostConfig.PublishAllPorts // false' "$inspect")" == "true" ]] && cmd+=(-P)
  [[ "$(jq -r '.[0].HostConfig.OomKillDisable // false' "$inspect")" == "true" ]] && cmd+=(--oom-kill-disable)
  [[ "$(jq -r '.[0].HostConfig.Init // false' "$inspect")" == "true" ]] && cmd+=(--init)

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--env "$item")
  done < <(jq -r '.[0].Config.Env[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--label "$item")
  done < <(jq -r '.[0].Config.Labels // {} | to_entries[] | "\(.key)=\(.value)"' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--cap-add "$item")
  done < <(jq -r '.[0].HostConfig.CapAdd[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--cap-drop "$item")
  done < <(jq -r '.[0].HostConfig.CapDrop[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--security-opt "$item")
  done < <(jq -r '.[0].HostConfig.SecurityOpt[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--group-add "$item")
  done < <(jq -r '.[0].HostConfig.GroupAdd[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--add-host "$item")
  done < <(jq -r '.[0].HostConfig.ExtraHosts[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--dns "$item")
  done < <(jq -r '.[0].HostConfig.Dns[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--dns-search "$item")
  done < <(jq -r '.[0].HostConfig.DnsSearch[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--dns-option "$item")
  done < <(jq -r '.[0].HostConfig.DnsOptions[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--link "$item")
  done < <(jq -r '.[0].HostConfig.Links[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--volumes-from "$item")
  done < <(jq -r '.[0].HostConfig.VolumesFrom[]?' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--device-cgroup-rule "$item")
  done < <(jq -r '.[0].HostConfig.DeviceCgroupRules[]?' "$inspect")

  while IFS=$'\t' read -r host_path container_path perms; do
    [[ -n "$host_path" ]] || continue
    spec="$host_path"
    [[ -n "$container_path" ]] && spec="${spec}:${container_path}"
    [[ -n "$perms" ]] && spec="${spec}:${perms}"
    cmd+=(--device "$spec")
  done < <(jq -r '.[0].HostConfig.Devices // [] | .[] | [(.PathOnHost // ""), (.PathInContainer // ""), (.CgroupPermissions // "")] | @tsv' "$inspect")

  while IFS=$'\t' read -r mount_type source volume_name dest ro propagation; do
    [[ -n "$mount_type" ]] || continue
    mount_spec=""
    case "$mount_type" in
      bind)
        [[ -n "$source" && -n "$dest" ]] || continue
        mount_spec="type=bind,src=${source},dst=${dest}"
        [[ "$ro" == "true" ]] && mount_spec="${mount_spec},readonly"
        [[ -n "$propagation" ]] && mount_spec="${mount_spec},bind-propagation=${propagation}"
        ;;
      volume)
        [[ -n "$volume_name" && -n "$dest" ]] || continue
        mount_spec="type=volume,src=${volume_name},dst=${dest}"
        [[ "$ro" == "true" ]] && mount_spec="${mount_spec},readonly"
        ;;
      *)
        continue
        ;;
    esac
    cmd+=(--mount "$mount_spec")
  done < <(jq -r '.[0].Mounts // [] | .[] | [(.Type // ""), (.Source // ""), (.Name // ""), (.Destination // ""), ((.RW|not)|tostring), (.Propagation // "")] | @tsv' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--tmpfs "$item")
  done < <(jq -r '.[0].HostConfig.Tmpfs // {} | to_entries[] | "\(.key):\(.value)"' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--sysctl "$item")
  done < <(jq -r '.[0].HostConfig.Sysctls // {} | to_entries[] | "\(.key)=\(.value)"' "$inspect")

  while IFS= read -r item; do
    [[ -n "$item" ]] && cmd+=(--ulimit "$item")
  done < <(jq -r '.[0].HostConfig.Ulimits // [] | .[] | if (.Soft != null and .Hard != null) then "\(.Name)=\(.Soft):\(.Hard)" else "\(.Name)" end' "$inspect")

  while IFS=$'\t' read -r host_ip host_port container_port; do
    [[ -n "$container_port" ]] || continue
    spec="$container_port"
    if [[ -n "$host_port" ]]; then
      if [[ -n "$host_ip" ]]; then
        spec="${host_ip}:${host_port}:${container_port}"
      else
        spec="${host_port}:${container_port}"
      fi
    fi
    cmd+=(--publish "$spec")
  done < <(jq -r '.[0].HostConfig.PortBindings // {} | to_entries[] | .key as $container | (.value // [])[] | [(.HostIp // ""), (.HostPort // ""), $container] | @tsv' "$inspect")

  restart_name="$(jq -r '.[0].HostConfig.RestartPolicy.Name // empty' "$inspect")"
  restart_max="$(jq -r '.[0].HostConfig.RestartPolicy.MaximumRetryCount // 0' "$inspect")"
  if [[ -n "$restart_name" && "$restart_name" != "no" ]]; then
    if [[ "$restart_name" == "on-failure" && "$restart_max" -gt 0 ]]; then
      cmd+=(--restart "${restart_name}:${restart_max}")
    else
      cmd+=(--restart "$restart_name")
    fi
  fi

  network_mode="$(jq -r '.[0].HostConfig.NetworkMode // empty' "$inspect")"
  if [[ -n "$network_mode" && "$network_mode" != "default" && "$network_mode" != "bridge" ]]; then
    cmd+=(--network "$network_mode")
  fi

  cmd+=("$backup_image")
  if [[ -n "$create_stderr_file" ]]; then
    "${cmd[@]}" >/dev/null 2>"$create_stderr_file"
  else
    "${cmd[@]}" >>"$EXECUTION_LOG_FILE" 2>&1
  fi
}

connect_additional_networks() {
  local inspect=$1 name=$2 primary=$3

  while IFS= read -r network_name; do
    [[ -n "$network_name" ]] || continue
    [[ "$network_name" == "bridge" || "$network_name" == "host" || "$network_name" == "none" ]] && continue
    [[ -n "$primary" && "$network_name" == "$primary" ]] && continue

    connect_cmd=(docker network connect)

    while IFS= read -r alias; do
      [[ -n "$alias" ]] && connect_cmd+=(--alias "$alias")
    done < <(jq -r --arg n "$network_name" '.[0].NetworkSettings.Networks[$n].Aliases[]?' "$inspect")

    ip="$(jq -r --arg n "$network_name" '.[0].NetworkSettings.Networks[$n].IPAddress // empty' "$inspect")"
    ip6="$(jq -r --arg n "$network_name" '.[0].NetworkSettings.Networks[$n].GlobalIPv6Address // empty' "$inspect")"
    [[ -n "$ip" ]] && connect_cmd+=(--ip "$ip")
    [[ -n "$ip6" ]] && connect_cmd+=(--ip6 "$ip6")

    connect_cmd+=("$network_name" "$name")
    "${connect_cmd[@]}" >>"$EXECUTION_LOG_FILE" 2>&1
  done < <(jq -r '.[0].NetworkSettings.Networks | keys[]?' "$inspect")
}

preflight_report_add() {
  printf '%s\n' "$1" >>"$preflight_report"
  validation_log "$1"
}

check_container_dependency_exists() {
  local dependency_name=$1

  [[ -z "$dependency_name" ]] && return 0
  if [[ -n "${backup_container_name_list:-}" ]] && printf '%s\n' "$backup_container_name_list" | grep -Fxq "$dependency_name"; then
    return 0
  fi
  docker container inspect "$dependency_name" >/dev/null 2>&1
}

run_restore_preflight() {
  local issues=0
  local container_dir container_name backup_image inspect_file meta_file image_file
  local volume_file volume_name archive_file mountpoint bind_key bind_type bind_source bind_archive_rel
  local network_file network_name dep_name mode source_path existing_type expected_type archive_abs

  preflight_report="$PREFLIGHT_REPORT_FILE"
  : >"$preflight_report"
  : >"$VALIDATION_LOG_FILE"
  validation_info "starting restore preflight"
  validation_info "backup_root=$backup_root"
  validation_info "expected containers=${#container_dirs[@]}, volumes=${#volume_files[@]}, networks=${#network_files[@]}"

  if ! is_valid_json "$backup_root/meta/manifest.json"; then
    preflight_report_add "manifest invalid or missing"
    issues=$((issues + 1))
  else
    validation_pass "manifest valid: $backup_root/meta/manifest.json"
  fi

  if [[ ! -s "$backup_root/meta/checksums.sha256" ]]; then
    preflight_report_add "checksums.sha256 missing or empty"
    issues=$((issues + 1))
  else
    validation_pass "checksums file present and non-empty: $backup_root/meta/checksums.sha256"
  fi

  for network_file in ${network_files[@]+"${network_files[@]}"}; do
    validation_info "checking network metadata file: $network_file"
    if ! is_valid_json "$network_file"; then
      preflight_report_add "network inspect invalid or missing: $network_file"
      issues=$((issues + 1))
      continue
    fi
    network_name="$(jq -r '.[0].Name // empty' "$network_file")"
    if [[ -z "$network_name" ]]; then
      preflight_report_add "network name missing in inspect: $network_file"
      issues=$((issues + 1))
    else
      validation_pass "network metadata valid: $network_name"
    fi
  done

  for volume_file in ${volume_files[@]+"${volume_files[@]}"}; do
    validation_info "checking volume metadata file: $volume_file"
    if ! is_valid_json "$volume_file"; then
      preflight_report_add "volume inspect invalid or missing: $volume_file"
      issues=$((issues + 1))
      continue
    fi

    volume_name="$(jq -r '.[0].Name // empty' "$volume_file")"
    if [[ -z "$volume_name" ]]; then
      preflight_report_add "volume name missing in inspect: $volume_file"
      issues=$((issues + 1))
      continue
    else
      validation_pass "volume metadata valid: $volume_name"
    fi

    archive_file="$backup_root/volumes/data/$(safe_name "$volume_name").tar"
    if [[ -f "$archive_file" ]] && ! is_valid_tar "$archive_file"; then
      preflight_report_add "volume archive invalid: $archive_file"
      issues=$((issues + 1))
    elif [[ -f "$archive_file" ]]; then
      validation_pass "volume archive valid: $archive_file"
    else
      validation_info "volume archive absent for metadata-only volume: $volume_name"
    fi

    if docker volume inspect "$volume_name" >/dev/null 2>&1; then
      mountpoint="$(docker volume inspect "$volume_name" 2>>"$VALIDATION_LOG_FILE" | jq -r '.[0].Mountpoint // empty')"
      validation_info "target volume exists on host: $volume_name mountpoint=$mountpoint"
      if [[ -n "$mountpoint" && -d "$mountpoint" && -f "$archive_file" ]]; then
        if find "$mountpoint" -mindepth 1 -print -quit | grep -q .; then
          preflight_report_add "target volume is not empty and would be overwritten: $volume_name ($mountpoint)"
          issues=$((issues + 1))
        else
          validation_pass "target volume is empty and safe to restore: $volume_name"
        fi
      fi
    else
      validation_info "target volume does not yet exist and will be created: $volume_name"
    fi
  done

  if [[ -f "$backup_root/meta/bind-mounts.tsv" ]]; then
    validation_pass "bind mount index present: $backup_root/meta/bind-mounts.tsv"
    while IFS=$'\t' read -r bind_key bind_type bind_source bind_archive_rel; do
      [[ -n "$bind_key" ]] || continue
      archive_abs="$backup_root/$bind_archive_rel"
      validation_info "checking bind source=$bind_source archive=$archive_abs expected_type=$bind_type"

      if [[ "$bind_source" != /* ]]; then
        preflight_report_add "bind source is not absolute: $bind_source"
        issues=$((issues + 1))
      fi

      if ! is_valid_tar "$archive_abs"; then
        preflight_report_add "bind archive invalid or missing: $archive_abs"
        issues=$((issues + 1))
      else
        validation_pass "bind archive valid: $archive_abs"
      fi

      if [[ -e "$bind_source" ]]; then
        if [[ -d "$bind_source" ]]; then
          existing_type="dir"
        else
          existing_type="file"
        fi

        expected_type="$bind_type"
        if [[ "$expected_type" != "$existing_type" ]]; then
          preflight_report_add "bind source type mismatch on target: $bind_source expected=$expected_type actual=$existing_type"
          issues=$((issues + 1))
        else
          validation_pass "bind source type matches target: $bind_source type=$existing_type"
        fi
      else
        validation_info "bind source does not yet exist on target and will be restored: $bind_source"
      fi
    done <"$backup_root/meta/bind-mounts.tsv"
  else
    validation_info "bind mount index absent; no bind mount data to validate"
  fi

  backup_container_name_list=""
  for container_dir in "${container_dirs[@]}"; do
    meta_file="$container_dir/meta.json"
    if is_valid_json "$meta_file"; then
      container_name="$(jq -r '.name // empty' "$meta_file")"
      if [[ -n "$container_name" ]]; then
        if [[ -n "$backup_container_name_list" ]]; then
          backup_container_name_list="${backup_container_name_list}"$'\n'"${container_name}"
        else
          backup_container_name_list="${container_name}"
        fi
      fi
    fi
  done

  for container_dir in "${container_dirs[@]}"; do
    meta_file="$container_dir/meta.json"
    inspect_file="$container_dir/inspect.json"
    image_file="$container_dir/image.tar"
    validation_info "checking container backup directory: $container_dir"

    if ! is_valid_json "$meta_file"; then
      preflight_report_add "container meta invalid or missing: $container_dir/meta.json"
      issues=$((issues + 1))
      continue
    else
      validation_pass "container metadata valid: $container_dir/meta.json"
    fi

    if ! is_valid_json "$inspect_file"; then
      preflight_report_add "container inspect invalid or missing: $container_dir/inspect.json"
      issues=$((issues + 1))
      continue
    else
      validation_pass "container inspect valid: $container_dir/inspect.json"
    fi

    if ! is_valid_tar "$image_file"; then
      preflight_report_add "container image archive invalid or missing: $container_dir/image.tar"
      issues=$((issues + 1))
    else
      validation_pass "container image archive valid: $image_file"
    fi

    container_name="$(jq -r '.name // empty' "$meta_file")"
    backup_image="$(jq -r '.backup_image // empty' "$meta_file")"
    if [[ -z "$container_name" ]]; then
      preflight_report_add "container name missing in meta: $meta_file"
      issues=$((issues + 1))
      continue
    else
      validation_pass "container name present: $container_name"
    fi

    [[ -n "$backup_image" ]] || {
      preflight_report_add "backup_image missing in meta: $meta_file"
      issues=$((issues + 1))
    }
    [[ -n "$backup_image" ]] && validation_pass "backup image recorded for container: $container_name -> $backup_image"

    if docker container inspect "$container_name" >/dev/null 2>&1; then
      preflight_report_add "target container already exists: $container_name"
      issues=$((issues + 1))
    else
      validation_pass "target container does not yet exist: $container_name"
    fi

    while IFS= read -r source_path; do
      [[ -n "$source_path" ]] || continue
      if [[ ! -e "$source_path" ]]; then
        preflight_report_add "required host device path missing: $container_name -> $source_path"
        issues=$((issues + 1))
      else
        validation_pass "required host device path exists: $container_name -> $source_path"
      fi
    done < <(jq -r '.[0].HostConfig.Devices // [] | .[] | .PathOnHost // empty' "$inspect_file")

    mode="$(jq -r '.[0].HostConfig.NetworkMode // empty' "$inspect_file")"
    if [[ "$mode" == container:* ]]; then
      dep_name="${mode#container:}"
      if ! check_container_dependency_exists "$dep_name"; then
        preflight_report_add "container network dependency missing: $container_name -> $dep_name"
        issues=$((issues + 1))
      else
        validation_pass "container network dependency satisfied: $container_name -> $dep_name"
      fi
    fi

    while IFS= read -r dep_name; do
      [[ -n "$dep_name" ]] || continue
      if ! check_container_dependency_exists "$dep_name"; then
        preflight_report_add "volumes-from dependency missing: $container_name -> $dep_name"
        issues=$((issues + 1))
      else
        validation_pass "volumes-from dependency satisfied: $container_name -> $dep_name"
      fi
    done < <(jq -r '.[0].HostConfig.VolumesFrom[]?' "$inspect_file" | sed 's/:.*$//')

    while IFS= read -r dep_name; do
      [[ -n "$dep_name" ]] || continue
      if ! check_container_dependency_exists "$dep_name"; then
        preflight_report_add "link dependency missing: $container_name -> $dep_name"
        issues=$((issues + 1))
      else
        validation_pass "link dependency satisfied: $container_name -> $dep_name"
      fi
    done < <(jq -r '.[0].HostConfig.Links[]?' "$inspect_file" | sed 's/:.*$//' | sed 's#^/##')
  done

  if (( issues > 0 )); then
    log "restore preflight failed, report: $preflight_report"
    validation_fail "restore preflight failed with issues=$issues; see $preflight_report"
    return 1
  fi

  printf 'restore preflight check passed\n' >"$preflight_report"
  validation_pass "restore preflight check passed with zero issues"
  log "preflight check passed"
  return 0
}

PREFLIGHT_ONLY=0
INPUT=""

while (($# > 0)); do
  case "$1" in
    --preflight-only)
      PREFLIGHT_ONLY=1
      shift
      ;;
    -h|--help)
      cat <<'EOF'
Usage:
  restore-docker-host.sh [--preflight-only] <backup.tar.gz | extracted-backup-dir>
EOF
      exit 0
      ;;
    *)
      if [[ -z "$INPUT" ]]; then
        INPUT="$1"
        shift
      else
        die "unexpected argument: $1"
      fi
      ;;
  esac
done

[[ -n "$INPUT" ]] || die "usage: $0 [--preflight-only] <backup.tar.gz | extracted-backup-dir>"

require_root
need_cmd docker
need_cmd jq
need_cmd tar
need_cmd gzip
need_cmd mktemp
need_cmd awk

workdir=""
cleanup() {
  if [[ -n "$workdir" && -d "$workdir" ]]; then
    rm -rf "$workdir"
  fi

  if [[ "$SCRIPT_STATUS" != "completed" ]]; then
    step_fail "restore exited before completion"
  fi

  write_summary
  return 0
}
trap cleanup EXIT

if [[ -f "$INPUT" ]]; then
  workdir="$(mktemp -d /tmp/docker-host-restore.XXXXXX)"
  log "extracting archive to $workdir"
  tar -xzpf "$INPUT" -C "$workdir" >>"$EXECUTION_LOG_FILE" 2>&1
  roots=()
  while IFS= read -r root_dir; do
    roots+=("$root_dir")
  done < <(find "$workdir" -mindepth 1 -maxdepth 1 -type d | sort)
  [[ "${#roots[@]}" -eq 1 ]] || die "cannot determine backup root after extraction"
  backup_root="${roots[0]}"
elif [[ -d "$INPUT" ]]; then
  backup_root="$INPUT"
else
  die "input is not a file or directory: $INPUT"
fi

[[ -d "$backup_root/meta" ]] || die "invalid backup directory: $backup_root"

if [[ -f "$INPUT" ]]; then
  restore_log_base="$(basename "$INPUT")"
  restore_log_dir="$(dirname "$INPUT")"
else
  restore_log_base="$(basename "$backup_root")"
  restore_log_dir="$backup_root/meta"
fi
EXECUTION_LOG_FILE="$restore_log_dir/${restore_log_base}.restore.execution.log"
SUMMARY_FILE="$restore_log_dir/${restore_log_base}.restore.summary.log"
PREFLIGHT_REPORT_FILE="$restore_log_dir/${restore_log_base}.restore.preflight.log"
VALIDATION_LOG_FILE="$restore_log_dir/${restore_log_base}.restore.validation.log"
: >"$EXECUTION_LOG_FILE"
log "execution log: $EXECUTION_LOG_FILE"
log "summary log: $SUMMARY_FILE"
log "preflight report: $PREFLIGHT_REPORT_FILE"
log "validation log: $VALIDATION_LOG_FILE"

declare -a network_files=()
declare -a volume_files=()
declare -a container_dirs=()
declare -a image_files=()

network_files=()
while IFS= read -r network_file; do
  network_files+=("$network_file")
done < <(find "$backup_root/networks" -type f -name '*.json' | sort)

volume_files=()
while IFS= read -r volume_file; do
  volume_files+=("$volume_file")
done < <(find "$backup_root/volumes/meta" -type f -name '*.json' | sort)

container_dirs=()
while IFS= read -r container_dir; do
  container_dirs+=("$container_dir")
done < <(find "$backup_root/containers" -mindepth 1 -maxdepth 1 -type d | sort)

image_files=()
while IFS= read -r image_file; do
  image_files+=("$image_file")
done < <(find "$backup_root/containers" -type f -name 'image.tar' | sort)

((${#container_dirs[@]} > 0)) || die "no container backups found"

container_count=${#container_dirs[@]}
volume_count=${#volume_files[@]}
network_count=${#network_files[@]}
step_info "discovered containers=${container_count}, volumes=${volume_count}, networks=${network_count}"

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
    printf 'backup_root=%s\n' "${backup_root:-}"
    printf 'preflight_report=%s\n' "${preflight_report:-}"
    printf 'execution_log=%s\n' "${EXECUTION_LOG_FILE:-}"
    printf 'validation_log=%s\n' "${VALIDATION_LOG_FILE:-}"
    printf 'containers=%s\n' "${container_count:-0}"
    printf 'volumes=%s\n' "${volume_count:-0}"
    printf 'networks=%s\n' "${network_count:-0}"
  } >"$SUMMARY_FILE"

  SUMMARY_WRITTEN=1
  log "summary written: $SUMMARY_FILE"
}

step_start "running restore preflight"
run_restore_preflight
step_success "restore preflight passed"

if [[ "$PREFLIGHT_ONLY" == "1" ]]; then
  SCRIPT_STATUS="completed"
  write_summary
  log "preflight-only mode completed successfully"
  exit 0
fi

step_start "loading committed images"
for image_file in ${image_files[@]+"${image_files[@]}"}; do
  step_info "loading image archive: $image_file"
  docker load -i "$image_file" >>"$EXECUTION_LOG_FILE" 2>&1
done
step_success "loaded committed images"

step_start "recreating custom networks"
for network_file in ${network_files[@]+"${network_files[@]}"}; do
  step_info "recreating network from: $network_file"
  create_network_from_inspect "$network_file"
done
step_success "recreated custom networks"

step_start "recreating volumes"
for volume_file in ${volume_files[@]+"${volume_files[@]}"}; do
  step_info "recreating volume from: $volume_file"
  create_volume_from_inspect "$volume_file"
done
step_success "recreated volumes"

step_start "restoring volume data"
for volume_file in ${volume_files[@]+"${volume_files[@]}"}; do
  volume_name="$(jq -r '.[0].Name' "$volume_file")"
  archive_file="$backup_root/volumes/data/$(safe_name "$volume_name").tar"
  [[ -f "$archive_file" ]] || continue

  step_info "restoring volume data: $volume_name"
  mountpoint="$(docker volume inspect "$volume_name" 2>>"$EXECUTION_LOG_FILE" | jq -r '.[0].Mountpoint')"
  [[ -n "$mountpoint" && -d "$mountpoint" ]] || die "volume mountpoint not accessible: $volume_name"

  if find "$mountpoint" -mindepth 1 -print -quit | grep -q .; then
    die "volume is not empty, refuse to overwrite: $volume_name ($mountpoint)"
  fi

  tar --numeric-owner --xattrs --acls -xpf "$archive_file" -C "$mountpoint" >>"$EXECUTION_LOG_FILE" 2>&1
done
step_success "restored volume data"

if [[ -f "$backup_root/meta/bind-mounts.tsv" ]]; then
  step_start "restoring bind mount data to original absolute paths"
  while IFS=$'\t' read -r bind_key bind_type bind_source bind_archive_rel; do
    [[ -n "$bind_key" ]] || continue
    archive_abs="$backup_root/$bind_archive_rel"
    [[ -f "$archive_abs" ]] || die "bind archive missing: $archive_abs"
    step_info "restoring bind mount data: $bind_source"
    tar --numeric-owner --xattrs --acls -xpf "$archive_abs" -C / >>"$EXECUTION_LOG_FILE" 2>&1
  done < "$backup_root/meta/bind-mounts.tsv"
  step_success "restored bind mount data"
else
  step_info "no bind mount archive index found"
fi

for container_dir in "${container_dirs[@]}"; do
  container_name="$(jq -r '.name' "$container_dir/meta.json")"
  docker container inspect "$container_name" >/dev/null 2>&1 && die "container already exists: $container_name"
done

step_start "creating containers"
pending=("${container_dirs[@]}")
for round in $(seq 1 10); do
  ((${#pending[@]} == 0)) && break
  next_pending=()
  progress=0
  step_info "container creation round ${round}, pending=${#pending[@]}"

  for container_dir in "${pending[@]}"; do
    container_name="$(jq -r '.name' "$container_dir/meta.json")"
    backup_image="$(jq -r '.backup_image' "$container_dir/meta.json")"
    inspect_file="$container_dir/inspect.json"

    step_info "creating container: $container_name"
    if create_container_from_inspect "$inspect_file" "$backup_image" "$container_name" "$container_dir/create.err"; then
      primary_network="$(jq -r '.[0].HostConfig.NetworkMode // empty' "$inspect_file")"
      if [[ "$primary_network" == "default" || "$primary_network" == "bridge" ]]; then
        primary_network=""
      fi
      connect_additional_networks "$inspect_file" "$container_name" "$primary_network" || true
      progress=1
      step_success "created container: $container_name"
    else
      next_pending+=("$container_dir")
      step_info "container create deferred: $container_name"
    fi
  done

  pending=("${next_pending[@]}")
  (( progress == 1 )) || break
done

if ((${#pending[@]} > 0)); then
  log "failed to create some containers"
  for container_dir in "${pending[@]}"; do
    container_name="$(jq -r '.name' "$container_dir/meta.json")"
    log "create failed: $container_name"
    if [[ -s "$container_dir/create.err" ]]; then
      while IFS= read -r err_line; do
        log "create stderr [$container_name] $err_line"
      done <"$container_dir/create.err"
    fi
  done
  exit 1
fi
step_success "created containers"

step_start "starting containers that were running before backup"
pending_start=()
for container_dir in "${container_dirs[@]}"; do
  was_running="$(jq -r '.running_before' "$container_dir/meta.json")"
  [[ "$was_running" == "true" ]] && pending_start+=("$container_dir")
done

if ((${#pending_start[@]} == 0)); then
  step_info "no containers were marked as running before backup"
fi

for round in $(seq 1 10); do
  ((${#pending_start[@]} == 0)) && break
  next_pending=()
  progress=0
  step_info "container start round ${round}, pending=${#pending_start[@]}"

  for container_dir in "${pending_start[@]}"; do
    container_name="$(jq -r '.name' "$container_dir/meta.json")"
    step_info "starting container: $container_name"
    if docker start "$container_name" >"$container_dir/start.out" 2>"$container_dir/start.err"; then
      progress=1
      step_success "started container: $container_name"
    else
      next_pending+=("$container_dir")
      step_info "container start deferred: $container_name"
    fi
  done

  pending_start=("${next_pending[@]}")
  (( progress == 1 )) || break
done

if ((${#pending_start[@]} > 0)); then
  log "failed to start some containers"
  for container_dir in "${pending_start[@]}"; do
    container_name="$(jq -r '.name' "$container_dir/meta.json")"
    log "start failed: $container_name"
    if [[ -s "$container_dir/start.err" ]]; then
      while IFS= read -r err_line; do
        log "start stderr [$container_name] $err_line"
      done <"$container_dir/start.err"
    fi
  done
  exit 1
fi

step_success "started containers that were running before backup"
SCRIPT_STATUS="completed"
write_summary
log "restore completed"
