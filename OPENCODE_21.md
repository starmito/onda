# OpenCode Task: Clean Deploy + Test + Investigate GPU Hang on .21

Machine: .21 192.168.1.21 ROCm AMD Radeon 780M gfx1103 4GB VRAM
Repo: /home/starmito/onda
Branch: feat/v3.0.0

## Part 1: Clean Deploy

Run in order:

git pull

docker compose -f docker-compose.yml -f docker-compose.rocm.yml down -v
docker rm -f onda
docker volume rm onda_pytorch-cache
docker rmi onda:3.1.1
rm -rf output/* input/*
mkdir -p output input

docker compose -f docker-compose.yml -f docker-compose.rocm.yml build --no-cache

docker compose -f docker-compose.yml -f docker-compose.rocm.yml up -d
sleep 10

docker ps --filter name=onda --format {{.Names}} {{.Status}}
docker exec onda id
docker logs onda --tail 5

## Part 2: Run All Tests

bash scripts/run-tests.sh

## Part 3: Investigate GPU Hang (only if inference tests fail)

If BS-Roformer or Demucs fail:

1. Check GPU detection:
   docker exec onda python3 -c "import torch; print(torch.cuda.is_available()); print(torch.cuda.get_device_name(0) if torch.cuda.is_available() else 'no cuda')"

2. Check ROCm version in container vs host:
   docker exec onda cat /opt/rocm/.info/version 2>/dev/null || echo 'no rocm info'
   echo '---host rocm---'
   dpkg -l | grep rocm-headers 2>/dev/null || cat /opt/rocm/.info/version 2>/dev/null || echo 'no host rocm info'

3. Check HSA_OVERRIDE:
   docker exec onda env | grep HSA

4. Test basic GPU tensor:
   docker exec onda python3 -c "
import torch
try:
    x = torch.randn(1, 2, 44100, device='cuda')
    print('GPU tensor OK', x.device)
    del x
except Exception as e:
    print('GPU error:', e)
"

## Rules

- If all tests pass: commit and push any changes you made. Report success.
- If inference fails: document the exact error, what you found in Part 3 checks, and possible solutions.
- Use conventional commits: fix: feat: chore: test:
- Do NOT leave uncommitted changes.
- Report findings back to the user.
