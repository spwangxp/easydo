#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
shopt -s nullglob

log() { printf '[%s] %s\n' "$(date '+%F %T')" "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }
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
  "${cmd[@]}"
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
  "${cmd[@]}"
}

create_container_from_inspect() {
  local inspect=$1 backup_image=$2 name=$3
  local network_mode restart_name restart_max

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
  "${cmd[@]}" >/dev/null
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
    "${connect_cmd[@]}" >/dev/null
  done < <(jq -r '.[0].NetworkSettings.Networks | keys[]?' "$inspect")
}

preflight_report_add() {
  printf '%s\n' "$1" >>"$preflight_report"
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

  preflight_report="$backup_root/meta/restore-preflight-report.txt"
  : >"$preflight_report"

  if ! is_valid_json "$backup_root/meta/manifest.json"; then
    preflight_report_add "manifest invalid or missing"
    issues=$((issues + 1))
  fi

  if [[ ! -s "$backup_root/meta/checksums.sha256" ]]; then
    preflight_report_add "checksums.sha256 missing or empty"
    issues=$((issues + 1))
  fi

  for network_file in ${network_files[@]+"${network_files[@]}"}; do
    if ! is_valid_json "$network_file"; then
      preflight_report_add "network inspect invalid or missing: $network_file"
      issues=$((issues + 1))
      continue
    fi
    network_name="$(jq -r '.[0].Name // empty' "$network_file")"
    if [[ -z "$network_name" ]]; then
      preflight_report_add "network name missing in inspect: $network_file"
      issues=$((issues + 1))
    fi
  done

  for volume_file in ${volume_files[@]+"${volume_files[@]}"}; do
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
    fi

    archive_file="$backup_root/volumes/data/$(safe_name "$volume_name").tar"
    if [[ -f "$archive_file" ]] && ! is_valid_tar "$archive_file"; then
      preflight_report_add "volume archive invalid: $archive_file"
      issues=$((issues + 1))
    fi

    if docker volume inspect "$volume_name" >/dev/null 2>&1; then
      mountpoint="$(docker volume inspect "$volume_name" | jq -r '.[0].Mountpoint // empty')"
      if [[ -n "$mountpoint" && -d "$mountpoint" && -f "$archive_file" ]]; then
        if find "$mountpoint" -mindepth 1 -print -quit | grep -q .; then
          preflight_report_add "target volume is not empty and would be overwritten: $volume_name ($mountpoint)"
          issues=$((issues + 1))
        fi
      fi
    fi
  done

  if [[ -f "$backup_root/meta/bind-mounts.tsv" ]]; then
    while IFS=$'\t' read -r bind_key bind_type bind_source bind_archive_rel; do
      [[ -n "$bind_key" ]] || continue
      archive_abs="$backup_root/$bind_archive_rel"

      if [[ "$bind_source" != /* ]]; then
        preflight_report_add "bind source is not absolute: $bind_source"
        issues=$((issues + 1))
      fi

      if ! is_valid_tar "$archive_abs"; then
        preflight_report_add "bind archive invalid or missing: $archive_abs"
        issues=$((issues + 1))
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
        fi
      fi
    done <"$backup_root/meta/bind-mounts.tsv"
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

    if ! is_valid_json "$meta_file"; then
      preflight_report_add "container meta invalid or missing: $container_dir/meta.json"
      issues=$((issues + 1))
      continue
    fi

    if ! is_valid_json "$inspect_file"; then
      preflight_report_add "container inspect invalid or missing: $container_dir/inspect.json"
      issues=$((issues + 1))
      continue
    fi

    if ! is_valid_tar "$image_file"; then
      preflight_report_add "container image archive invalid or missing: $container_dir/image.tar"
      issues=$((issues + 1))
    fi

    container_name="$(jq -r '.name // empty' "$meta_file")"
    backup_image="$(jq -r '.backup_image // empty' "$meta_file")"
    if [[ -z "$container_name" ]]; then
      preflight_report_add "container name missing in meta: $meta_file"
      issues=$((issues + 1))
      continue
    fi

    [[ -n "$backup_image" ]] || {
      preflight_report_add "backup_image missing in meta: $meta_file"
      issues=$((issues + 1))
    }

    if docker container inspect "$container_name" >/dev/null 2>&1; then
      preflight_report_add "target container already exists: $container_name"
      issues=$((issues + 1))
    fi

    while IFS= read -r source_path; do
      [[ -n "$source_path" ]] || continue
      if [[ ! -e "$source_path" ]]; then
        preflight_report_add "required host device path missing: $container_name -> $source_path"
        issues=$((issues + 1))
      fi
    done < <(jq -r '.[0].HostConfig.Devices // [] | .[] | .PathOnHost // empty' "$inspect_file")

    mode="$(jq -r '.[0].HostConfig.NetworkMode // empty' "$inspect_file")"
    if [[ "$mode" == container:* ]]; then
      dep_name="${mode#container:}"
      if ! check_container_dependency_exists "$dep_name"; then
        preflight_report_add "container network dependency missing: $container_name -> $dep_name"
        issues=$((issues + 1))
      fi
    fi

    while IFS= read -r dep_name; do
      [[ -n "$dep_name" ]] || continue
      if ! check_container_dependency_exists "$dep_name"; then
        preflight_report_add "volumes-from dependency missing: $container_name -> $dep_name"
        issues=$((issues + 1))
      fi
    done < <(jq -r '.[0].HostConfig.VolumesFrom[]?' "$inspect_file" | sed 's/:.*$//')

    while IFS= read -r dep_name; do
      [[ -n "$dep_name" ]] || continue
      if ! check_container_dependency_exists "$dep_name"; then
        preflight_report_add "link dependency missing: $container_name -> $dep_name"
        issues=$((issues + 1))
      fi
    done < <(jq -r '.[0].HostConfig.Links[]?' "$inspect_file" | sed 's/:.*$//' | sed 's#^/##')
  done

  if (( issues > 0 )); then
    log "restore preflight failed, report: $preflight_report"
    cat "$preflight_report" >&2
    return 1
  fi

  printf 'restore preflight check passed\n' >"$preflight_report"
  log "preflight check passed"
  return 0
}

require_root
need_cmd docker
need_cmd jq
need_cmd tar
need_cmd gzip
need_cmd mktemp
need_cmd awk

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

workdir=""
cleanup() {
  if [[ -n "$workdir" && -d "$workdir" ]]; then
    rm -rf "$workdir"
  fi
  return 0
}
trap cleanup EXIT

if [[ -f "$INPUT" ]]; then
  workdir="$(mktemp -d /tmp/docker-host-restore.XXXXXX)"
  log "extracting archive to $workdir"
  tar -xzpf "$INPUT" -C "$workdir"
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

run_restore_preflight

if [[ "$PREFLIGHT_ONLY" == "1" ]]; then
  log "preflight-only mode completed successfully"
  exit 0
fi

log "loading committed images"
for image_file in ${image_files[@]+"${image_files[@]}"}; do
  docker load -i "$image_file" >/dev/null
done

log "recreating custom networks"
for network_file in ${network_files[@]+"${network_files[@]}"}; do
  create_network_from_inspect "$network_file"
done

log "recreating volumes"
for volume_file in ${volume_files[@]+"${volume_files[@]}"}; do
  create_volume_from_inspect "$volume_file"
done

log "restoring volume data"
for volume_file in ${volume_files[@]+"${volume_files[@]}"}; do
  volume_name="$(jq -r '.[0].Name' "$volume_file")"
  archive_file="$backup_root/volumes/data/$(safe_name "$volume_name").tar"
  [[ -f "$archive_file" ]] || continue

  mountpoint="$(docker volume inspect "$volume_name" | jq -r '.[0].Mountpoint')"
  [[ -n "$mountpoint" && -d "$mountpoint" ]] || die "volume mountpoint not accessible: $volume_name"

  if find "$mountpoint" -mindepth 1 -print -quit | grep -q .; then
    die "volume is not empty, refuse to overwrite: $volume_name ($mountpoint)"
  fi

  tar --numeric-owner --xattrs --acls -xpf "$archive_file" -C "$mountpoint"
done

if [[ -f "$backup_root/meta/bind-mounts.tsv" ]]; then
  log "restoring bind mount data to original absolute paths"
  while IFS=$'\t' read -r bind_key bind_type bind_source bind_archive_rel; do
    [[ -n "$bind_key" ]] || continue
    archive_abs="$backup_root/$bind_archive_rel"
    [[ -f "$archive_abs" ]] || die "bind archive missing: $archive_abs"
    tar --numeric-owner --xattrs --acls -xpf "$archive_abs" -C /
  done < "$backup_root/meta/bind-mounts.tsv"
fi

for container_dir in "${container_dirs[@]}"; do
  container_name="$(jq -r '.name' "$container_dir/meta.json")"
  docker container inspect "$container_name" >/dev/null 2>&1 && die "container already exists: $container_name"
done

log "creating containers"
pending=("${container_dirs[@]}")
for round in $(seq 1 10); do
  ((${#pending[@]} == 0)) && break
  next_pending=()
  progress=0

  for container_dir in "${pending[@]}"; do
    container_name="$(jq -r '.name' "$container_dir/meta.json")"
    backup_image="$(jq -r '.backup_image' "$container_dir/meta.json")"
    inspect_file="$container_dir/inspect.json"

    if create_container_from_inspect "$inspect_file" "$backup_image" "$container_name" 2>"$container_dir/create.err"; then
      primary_network="$(jq -r '.[0].HostConfig.NetworkMode // empty' "$inspect_file")"
      if [[ "$primary_network" == "default" || "$primary_network" == "bridge" ]]; then
        primary_network=""
      fi
      connect_additional_networks "$inspect_file" "$container_name" "$primary_network" || true
      progress=1
    else
      next_pending+=("$container_dir")
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
    [[ -s "$container_dir/create.err" ]] && tail -n 20 "$container_dir/create.err" >&2
  done
  exit 1
fi

log "starting containers that were running before backup"
pending_start=()
for container_dir in "${container_dirs[@]}"; do
  was_running="$(jq -r '.running_before' "$container_dir/meta.json")"
  [[ "$was_running" == "true" ]] && pending_start+=("$container_dir")
done

for round in $(seq 1 10); do
  ((${#pending_start[@]} == 0)) && break
  next_pending=()
  progress=0

  for container_dir in "${pending_start[@]}"; do
    container_name="$(jq -r '.name' "$container_dir/meta.json")"
    if docker start "$container_name" >"$container_dir/start.out" 2>"$container_dir/start.err"; then
      progress=1
    else
      next_pending+=("$container_dir")
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
    [[ -s "$container_dir/start.err" ]] && tail -n 20 "$container_dir/start.err" >&2
  done
  exit 1
fi

log "restore completed"
