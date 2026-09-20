#!/usr/bin/env bash
#
# tools/check-licenses.sh — Guardián de licencias prohibidas + go mod tidy.
#
# Revisa:
#   (a) dependencias de PRODUCCION del frontal (frontend/package-lock.json)
#   (b) dependencias Python prohibidas (requirements-common.txt, requirements-docker.txt, requirements.lock)
#   (c) codigo vendorizado en lib_v5/ (cabeceras de licencia / ficheros LICENSE*)
#   (d) que `go mod tidy -diff` no produzca cambios en backend/go.mod+go.sum
#
# Criterio de fallo (bloquea, exit 1): AGPL-*, GPL-2.0, GPL-3.0, SSPL-*, BUSL-*
# Aviso (no bloquea): LGPL-*, CC-BY-NC-*, UNKNOWN
#
# Uso:
#   tools/check-licenses.sh
#
# Codigos de salida:
#   0 = sin bloqueos
#   1 = hay al menos una licencia bloqueante o go mod tidy -diff no esta vacio

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ -t 1 ]]; then
  C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_YEL=$'\033[33m'; C_BLD=$'\033[1m'; C_OFF=$'\033[0m'
else
  C_RED=""; C_GRN=""; C_YEL=""; C_BLD=""; C_OFF=""
fi

errors=0
warnings=0
infos=0

ok()   { printf '%s[OK]%s   %s\n' "$C_GRN" "$C_OFF" "$*"; }
warn() { printf '%s[WARN]%s %s\n' "$C_YEL" "$C_OFF" "$*"; warnings=$((warnings + 1)); }
fail() { printf '%s[FAIL]%s %s\n' "$C_RED" "$C_OFF" "$*"; errors=$((errors + 1)); }
info() { printf '%s[INFO]%s %s\n' "$C_BLD" "$C_OFF" "$*"; infos=$((infos + 1)); }

printf '%s== check-licenses: licencias + go mod tidy ==%s\n' "$C_BLD" "$C_OFF"

# ---------------------------------------------------------------- licencias
py_output=$(python3 - "$REPO_ROOT" <<'PY'
import json, os, re, sys

repo_root = sys.argv[1]

BLOCKING_PREFIXES = ['AGPL-', 'GPL-2.0', 'GPL-3.0', 'SSPL-', 'BUSL-']
WARNING_PREFIXES = ['LGPL-', 'CC-BY-NC-', 'UNKNOWN']

PY_FORBIDDEN = {'essentia', 'essentia-tensorflow'}

# Excepciones explicitas y justificadas una a una.
# La clave es la ruta relativa al repo.
EXCEPTIONS = {}

def normalize(lic):
    if not lic:
        return 'UNKNOWN'
    return lic.strip()

def matches(lic, prefixes):
    up = lic.upper()
    for p in prefixes:
        if up.startswith(p.upper()):
            return True
    return False

def detect_license_in_text(text):
    patterns = [
        (r'Affero\s+General\s+Public\s+License', 'AGPL'),
        (r'GNU\s+General\s+Public\s+License\s+as\s+published\s+by\s+the\s+Free\s+Software\s+Foundation,\s+either\s+version\s+3', 'GPL-3.0+'),
        (r'GNU\s+General\s+Public\s+License[^\n]*version\s+2', 'GPL-2.0+'),
        (r'GNU\s+Lesser\s+General\s+Public\s+License', 'LGPL'),
        (r'Server\s+Side\s+Public\s+License', 'SSPL'),
        (r'Business\s+Source\s+License', 'BUSL'),
        (r'Apache\s+License', 'Apache'),
        (r'MIT\s+License', 'MIT'),
        (r'BSD', 'BSD'),
    ]
    for pat, name in patterns:
        if re.search(pat, text, re.IGNORECASE):
            return name
    return None

exit_code = 0

# (a) Frontend: solo dependencias de produccion
print('')
print('[INFO] Frontend: dependencias de produccion (frontend/package-lock.json)')
lock_path = os.path.join(repo_root, 'frontend', 'package-lock.json')
if not os.path.isfile(lock_path):
    print('[FAIL] frontend/package-lock.json no encontrado')
    exit_code = 1
else:
    with open(lock_path, 'r', encoding='utf-8') as f:
        data = json.load(f)
    packages = data.get('packages', {})
    root = packages.get('', {})
    prod_names = list(root.get('dependencies', {}).keys())

    seen = set(prod_names)
    queue = list(prod_names)
    prod_pkgs = {}

    while queue:
        name = queue.pop(0)
        key = f'node_modules/{name}'
        pkg = packages.get(key)
        if pkg is None:
            for k, v in packages.items():
                if k.endswith(f'/node_modules/{name}') and v.get('name') == name:
                    pkg = v
                    break
        if pkg is None:
            continue
        prod_pkgs[name] = pkg
        for dep in pkg.get('dependencies', {}):
            if dep not in seen:
                seen.add(dep)
                queue.append(dep)

    if not prod_pkgs:
        print('[OK]   frontend/package-lock.json -> (sin dependencias de produccion)')
    for name in sorted(prod_pkgs):
        pkg = prod_pkgs[name]
        lic = normalize(pkg.get('license', ''))
        version = pkg.get('version', '?')
        line = f'frontend/package-lock.json -> {name}@{version} -> {lic}'
        if matches(lic, BLOCKING_PREFIXES):
            print(f'[FAIL] {line} -> BLOQUEA')
            exit_code = 1
        elif matches(lic, WARNING_PREFIXES):
            print(f'[WARN] {line} -> AVISO')
        else:
            print(f'[OK]   {line} -> OK')

# (b) Python: paquetes prohibidos
print('')
print('[INFO] Python: paquetes con licencia prohibida conocida')
req_files = ['requirements-common.txt', 'requirements-docker.txt', 'requirements.lock']
found_any = False
for req_file in req_files:
    path = os.path.join(repo_root, req_file)
    if not os.path.isfile(path):
        continue
    with open(path, 'r', encoding='utf-8') as f:
        for raw in f:
            line = raw.strip()
            if not line or line.startswith('#'):
                continue
            if '==' in line:
                pkg_name = line.split('==')[0].strip()
            else:
                pkg_name = line.split()[0].strip()
            norm = pkg_name.lower().replace('_', '-')
            base = norm.split('[')[0]
            for forbidden in PY_FORBIDDEN:
                if base == forbidden or base.startswith(forbidden + '-'):
                    print(f'[FAIL] {req_file} -> {pkg_name} -> FORBIDDEN ({forbidden})')
                    exit_code = 1
                    found_any = True
if not found_any:
    print('[OK]   requirements *.txt / requirements.lock -> ningun paquete prohibido encontrado')

# (c) Codigo vendorizado en lib_v5/
print('')
print('[INFO] Vendor: lib_v5/ (cabeceras de licencia / ficheros LICENSE*)')
vendor_dir = os.path.join(repo_root, 'lib_v5')
if not os.path.isdir(vendor_dir):
    print('[FAIL] lib_v5/ no encontrado')
    exit_code = 1
else:
    reported = False
    for root, dirs, files in os.walk(vendor_dir):
        dirs[:] = [d for d in dirs if d != '__pycache__']
        for fname in files:
            fpath = os.path.join(root, fname)
            relpath = os.path.relpath(fpath, repo_root)
            lower = fname.lower()
            if lower.startswith('license') or lower.startswith('copying') or lower.startswith('notice'):
                text = open(fpath, 'r', encoding='utf-8', errors='ignore').read()
                lic = detect_license_in_text(text) or 'UNKNOWN'
                line = f'{relpath} -> {lic}'
                if matches(lic, BLOCKING_PREFIXES):
                    print(f'[FAIL] {line} -> BLOQUEA')
                    exit_code = 1
                elif matches(lic, WARNING_PREFIXES):
                    print(f'[WARN] {line} -> AVISO')
                else:
                    print(f'[OK]   {line} -> OK')
                reported = True
            elif fname.endswith('.py'):
                text = open(fpath, 'r', encoding='utf-8', errors='ignore').read()
                lic = detect_license_in_text(text)
                if lic is None:
                    continue
                line = f'{relpath} -> {lic}'
                if relpath in EXCEPTIONS:
                    exc = EXCEPTIONS[relpath]
                    print(f'[INFO] {line} -> EXCEPTUADO')
                    print(f'[INFO]        fuente: {exc["source"]}')
                    print(f'[INFO]        motivo: {exc["reason"]}')
                    print(f'[INFO]        {exc["todo"]}')
                elif matches(lic, BLOCKING_PREFIXES):
                    print(f'[FAIL] {line} -> BLOQUEA')
                    exit_code = 1
                elif matches(lic, WARNING_PREFIXES):
                    print(f'[WARN] {line} -> AVISO')
                else:
                    print(f'[OK]   {line} -> OK')
                reported = True
    if not reported:
        print('[WARN] lib_v5/ -> no se detectaron cabeceras ni ficheros LICENSE*')

sys.exit(exit_code)
PY
)
py_exit=$?

printf '%s\n' "$py_output"

py_warns=$(printf '%s\n' "$py_output" | grep -c '^\[WARN\]' || true)
py_fails=$(printf '%s\n' "$py_output" | grep -c '^\[FAIL\]' || true)
warnings=$((warnings + py_warns))
errors=$((errors + py_fails))

if [[ $py_exit -ne 0 && $py_fails -eq 0 ]]; then
  fail 'check-licenses: el script Python de licencias fallo inesperadamente (ver salida arriba)'
fi

# ---------------------------------------------------------------- go mod tidy
printf '\n'
info 'Go: backend/go.mod + go.sum estan limpios (go mod tidy -diff)'

TMPD="$REPO_ROOT/.hermes/tmp"
mkdir -p "$TMPD"
GODIFF="$TMPD/gomod-tidy-diff.txt"
GOERR="$TMPD/gomod-tidy-err.txt"

if ! (cd "$REPO_ROOT/backend" && go mod tidy -diff > "$GODIFF" 2> "$GOERR"); then
  go_err=$(cat "$GOERR" 2>/dev/null || true)
  if echo "$go_err" | grep -qiE 'module lookup disabled|no such host|proxy|network|timeout|connection|unreachable|downloading.*failed|GOPROXY'; then
    warn 'go mod tidy -diff no pudo ejecutarse sin red o sin cache de modulos'
    info '       error de red/cache; no se considera fallo de licencias'
  else
    fail 'go mod tidy -diff: error inesperado'
    sed 's/^/       /' "$GOERR" || true
  fi
else
  if [[ -s "$GODIFF" ]]; then
    fail 'go mod tidy -diff: go.mod/go.sum NO estan limpios'
    sed 's/^/       /' "$GODIFF" || true
  else
    ok 'go mod tidy -diff: vacio'
  fi
fi

# ---------------------------------------------------------------- resumen
printf '\n'
if [[ $errors -gt 0 ]]; then
  printf '%sRESULTADO: %d fallo(s), %d aviso(s)%s\n' "$C_RED" "$errors" "$warnings" "$C_OFF"
  exit 1
fi
printf '%sRESULTADO: OK - %d fallos, %d aviso(s)%s\n' "$C_GRN" "$errors" "$warnings" "$C_OFF"
exit 0
