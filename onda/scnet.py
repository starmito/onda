"""onda scnet — SCNet source separation headless wrapper.

Headless reimplementation of the SCNet architecture from
ZFTurbo/Music-Source-Separation-Training with chunk-based overlap-add
inference matching inference_universal.py.
"""

import argparse
import glob
import json
import math
import os
import sys
import warnings
from collections import deque
from typing import Dict, List, Optional, Tuple

import yaml
import torch
import torch.nn as nn
import torch.nn.functional as F
import numpy as np
import librosa
import soundfile as sf


# ── Model architecture (from ZFTurbo/Music-Source-Separation-Training) ──


class Swish(nn.Module):
    def forward(self, x):
        return x * x.sigmoid()


class ConvolutionModule(nn.Module):
    """Convolution Module in SD block."""

    def __init__(self, channels, depth=2, compress=4, kernel=3):
        super().__init__()
        assert kernel % 2 == 1
        self.depth = abs(depth)
        hidden_size = int(channels / compress)
        norm = lambda d: nn.GroupNorm(1, d)
        self.layers = nn.ModuleList([])
        for _ in range(self.depth):
            padding = kernel // 2
            mods = [
                norm(channels),
                nn.Conv1d(channels, hidden_size * 2, kernel, padding=padding),
                nn.GLU(1),
                nn.Conv1d(hidden_size, hidden_size, kernel, padding=padding, groups=hidden_size),
                norm(hidden_size),
                Swish(),
                nn.Conv1d(hidden_size, channels, 1),
            ]
            self.layers.append(nn.Sequential(*mods))

    def forward(self, x):
        for layer in self.layers:
            x = x + layer(x)
        return x


class FusionLayer(nn.Module):
    """Fusion layer inside the decoder."""

    def __init__(self, channels, kernel_size=3, stride=1, padding=1):
        super().__init__()
        self.conv = nn.Conv2d(channels * 2, channels * 2, kernel_size, stride=stride, padding=padding)

    def forward(self, x, skip=None):
        if skip is not None:
            x += skip
        x = x.repeat(1, 2, 1, 1)
        x = self.conv(x)
        x = F.glu(x, dim=1)
        return x


class SDlayer(nn.Module):
    """Sparse Down-sample Layer."""

    def __init__(self, channels_in, channels_out, band_configs):
        super().__init__()
        self.convs = nn.ModuleList()
        self.strides = []
        self.kernels = []
        for config in band_configs.values():
            self.convs.append(
                nn.Conv2d(channels_in, channels_out, (config['kernel'], 1), (config['stride'], 1), (0, 0))
            )
            self.strides.append(config['stride'])
            self.kernels.append(config['kernel'])
        self.SR_low = band_configs['low']['SR']
        self.SR_mid = band_configs['mid']['SR']

    def forward(self, x):
        B, C, Fr, T = x.shape
        splits = [
            (0, math.ceil(Fr * self.SR_low)),
            (math.ceil(Fr * self.SR_low), math.ceil(Fr * (self.SR_low + self.SR_mid))),
            (math.ceil(Fr * (self.SR_low + self.SR_mid)), Fr),
        ]

        outputs = []
        original_lengths = []
        for conv, stride, kernel, (start, end) in zip(self.convs, self.strides, self.kernels, splits):
            extracted = x[:, :, start:end, :]
            original_lengths.append(end - start)
            current_length = extracted.shape[2]

            if stride == 1:
                total_padding = kernel - stride
            else:
                total_padding = (stride - current_length % stride) % stride
            pad_left = total_padding // 2
            pad_right = total_padding - pad_left
            padded = F.pad(extracted, (0, 0, pad_left, pad_right))
            outputs.append(conv(padded))

        return outputs, original_lengths


class SUlayer(nn.Module):
    """Sparse Up-sample Layer."""

    def __init__(self, channels_in, channels_out, band_configs):
        super().__init__()
        self.convtrs = nn.ModuleList([
            nn.ConvTranspose2d(channels_in, channels_out, [config['kernel'], 1], [config['stride'], 1])
            for _, config in band_configs.items()
        ])

    def forward(self, x, lengths, origin_lengths):
        splits = [
            (0, lengths[0]),
            (lengths[0], lengths[0] + lengths[1]),
            (lengths[0] + lengths[1], None),
        ]
        outputs = []
        for idx, (convtr, (start, end)) in enumerate(zip(self.convtrs, splits)):
            out = convtr(x[:, :, start:end, :])
            current_Fr_length = out.shape[2]
            dist = abs(origin_lengths[idx] - current_Fr_length) // 2
            trimmed_out = out[:, :, dist:dist + origin_lengths[idx], :]
            outputs.append(trimmed_out)
        return torch.cat(outputs, dim=2)


class SDblock(nn.Module):
    """Sparse Down-sample block."""

    def __init__(self, channels_in, channels_out, band_configs=None, conv_config=None, depths=(3, 2, 1), kernel_size=3):
        super().__init__()
        band_configs = band_configs or {}
        conv_config = conv_config or {}
        self.SDlayer = SDlayer(channels_in, channels_out, band_configs)
        self.conv_modules = nn.ModuleList([
            ConvolutionModule(channels_out, depth, **conv_config) for depth in depths
        ])
        self.globalconv = nn.Conv2d(channels_out, channels_out, kernel_size, 1, (kernel_size - 1) // 2)

    def forward(self, x):
        bands, original_lengths = self.SDlayer(x)
        bands = [
            F.gelu(
                conv(band.permute(0, 2, 1, 3).reshape(-1, band.shape[1], band.shape[3]))
                .view(band.shape[0], band.shape[2], band.shape[1], band.shape[3])
                .permute(0, 2, 1, 3)
            )
            for conv, band in zip(self.conv_modules, bands)
        ]
        lengths = [band.size(-2) for band in bands]
        full_band = torch.cat(bands, dim=2)
        skip = full_band
        output = self.globalconv(full_band)
        return output, skip, lengths, original_lengths


class FeatureConversion(nn.Module):
    """FFT feature conversion inside the separation network."""

    def __init__(self, channels, inverse):
        super().__init__()
        self.inverse = inverse
        self.channels = channels

    def forward(self, x):
        if self.inverse:
            x = x.float()
            x_r = x[:, :self.channels // 2, :, :]
            x_i = x[:, self.channels // 2:, :, :]
            x = torch.complex(x_r, x_i)
            x = torch.fft.irfft(x, dim=3, norm="ortho")
        else:
            x = x.float()
            x = torch.fft.rfft(x, dim=3, norm="ortho")
            x = torch.cat([x.real, x.imag], dim=1)
        return x


class DualPathRNN(nn.Module):
    """Dual-Path RNN used by SeparationNet."""

    def __init__(self, d_model, expand, bidirectional=True):
        super().__init__()
        self.d_model = d_model
        self.hidden_size = d_model * expand
        self.bidirectional = bidirectional
        self.lstm_layers = nn.ModuleList([self._init_lstm(self.d_model, self.hidden_size) for _ in range(2)])
        self.linear_layers = nn.ModuleList([nn.Linear(self.hidden_size * 2, self.d_model) for _ in range(2)])
        self.norm_layers = nn.ModuleList([nn.GroupNorm(1, d_model) for _ in range(2)])

    def _init_lstm(self, d_model, hidden_size):
        return nn.LSTM(d_model, hidden_size, num_layers=1, bidirectional=self.bidirectional, batch_first=True)

    def forward(self, x):
        B, C, F, T = x.shape
        original_x = x
        x = self.norm_layers[0](x)
        x = x.transpose(1, 3).contiguous().view(B * T, F, C)
        x, _ = self.lstm_layers[0](x)
        x = self.linear_layers[0](x)
        x = x.view(B, T, F, C).transpose(1, 3)
        x = x + original_x

        original_x = x
        x = self.norm_layers[1](x)
        x = x.transpose(1, 2).contiguous().view(B * F, C, T).transpose(1, 2)
        x, _ = self.lstm_layers[1](x)
        x = self.linear_layers[1](x)
        x = x.transpose(1, 2).contiguous().view(B, F, C, T).transpose(1, 2)
        x = x + original_x
        return x


class SeparationNet(nn.Module):
    """Separation network with stacked dual-path RNN blocks."""

    def __init__(self, channels, expand=1, num_layers=6):
        super().__init__()
        self.num_layers = num_layers
        self.dp_modules = nn.ModuleList([
            DualPathRNN(channels * (2 if i % 2 == 1 else 1), expand) for i in range(num_layers)
        ])
        self.feature_conversion = nn.ModuleList([
            FeatureConversion(channels * 2, inverse=False if i % 2 == 0 else True) for i in range(num_layers)
        ])

    def forward(self, x):
        for i in range(self.num_layers):
            x = self.dp_modules[i](x)
            x = self.feature_conversion[i](x)
        return x


class SCNet(nn.Module):
    """SCNet: Sparse Compression Network for Music Source Separation."""

    def __init__(
        self,
        sources=('drums', 'bass', 'other', 'vocals'),
        audio_channels=2,
        dims=(4, 32, 64, 128),
        nfft=4096,
        hop_size=1024,
        win_size=4096,
        normalized=True,
        band_SR=(0.175, 0.392, 0.433),
        band_stride=(1, 4, 16),
        band_kernel=(3, 4, 16),
        conv_depths=(3, 2, 1),
        compress=4,
        conv_kernel=3,
        num_dplayer=6,
        expand=1,
    ):
        super().__init__()
        self.sources = list(sources)
        self.audio_channels = audio_channels
        self.dims = list(dims)
        band_keys = ['low', 'mid', 'high']
        self.band_configs = {
            band_keys[i]: {'SR': band_SR[i], 'stride': band_stride[i], 'kernel': band_kernel[i]}
            for i in range(len(band_keys))
        }
        self.hop_length = hop_size
        self.conv_config = {'compress': compress, 'kernel': conv_kernel}
        self.stft_config = {
            'n_fft': nfft,
            'hop_length': hop_size,
            'win_length': win_size,
            'center': True,
            'normalized': normalized,
        }

        self.encoder = nn.ModuleList()
        self.decoder = nn.ModuleList()
        for index in range(len(self.dims) - 1):
            enc = SDblock(
                channels_in=self.dims[index],
                channels_out=self.dims[index + 1],
                band_configs=self.band_configs,
                conv_config=self.conv_config,
                depths=conv_depths,
            )
            self.encoder.append(enc)
            dec = nn.Sequential(
                FusionLayer(channels=self.dims[index + 1]),
                SUlayer(
                    channels_in=self.dims[index + 1],
                    channels_out=self.dims[index] if index != 0 else self.dims[index] * len(self.sources),
                    band_configs=self.band_configs,
                ),
            )
            self.decoder.insert(0, dec)

        self.separation_net = SeparationNet(channels=self.dims[-1], expand=expand, num_layers=num_dplayer)

    def forward(self, x):
        B = x.shape[0]
        padding = self.hop_length - x.shape[-1] % self.hop_length
        if (x.shape[-1] + padding) // self.hop_length % 2 == 0:
            padding += self.hop_length
        x = F.pad(x, (0, padding))

        L = x.shape[-1]
        x = x.reshape(-1, L)
        x = torch.stft(x, return_complex=True, **self.stft_config)
        x = torch.view_as_real(x)
        x = x.permute(0, 3, 1, 2).reshape(
            x.shape[0] // self.audio_channels, x.shape[3] * self.audio_channels, x.shape[1], x.shape[2]
        )

        save_skip = deque()
        save_lengths = deque()
        save_original_lengths = deque()
        for sd_layer in self.encoder:
            x, skip, lengths, original_lengths = sd_layer(x)
            save_skip.append(skip)
            save_lengths.append(lengths)
            save_original_lengths.append(original_lengths)

        x = self.separation_net(x)

        for fusion_layer, su_layer in self.decoder:
            x = fusion_layer(x, save_skip.pop())
            x = su_layer(x, save_lengths.pop(), save_original_lengths.pop())

        n = self.dims[0]
        x = x.view(B, n, -1, x.shape[-2], x.shape[-1])
        x = x.reshape(-1, 2, x.shape[-2], x.shape[-1]).permute(0, 2, 3, 1)
        x = torch.view_as_complex(x.contiguous())
        x = torch.istft(x, **self.stft_config)
        x = x.reshape(B, len(self.sources), self.audio_channels, -1)
        return x[:, :, :, :-padding]


# ── Headless inference helpers ──


def _load_config(config_path: str) -> dict:
    with open(config_path) as f:
        return yaml.full_load(f)


def _prepare_mix(audio_path: str) -> Tuple[np.ndarray, int]:
    """Load audio as (channels, samples) float32 at 44.1 kHz."""
    mix, sr = librosa.load(audio_path, mono=False, sr=44100)
    if mix.ndim == 1:
        mix = np.stack([mix, mix], axis=0)
    elif mix.shape[0] > 2:
        mix = mix[:2]
    return mix.astype(np.float32), sr


def _write_progress(progress_file: Optional[str], chunk: int, total: int):
    if not progress_file:
        return
    progress = chunk / total if total > 0 else 0.0
    try:
        with open(progress_file, 'w') as pf:
            pf.write(
                '{"step":"scnet","progress":%.4f,"chunk":%d,"total_chunks":%d}'
                % (progress, chunk, total)
            )
            pf.flush()
    except Exception:
        pass


def _write_pipeline_status(status_file: Optional[str], step: str, progress: float,
                           chunk: int, total: int, device: str):
    if not status_file:
        return
    try:
        if os.path.exists(status_file):
            with open(status_file) as f:
                data = json.load(f)
        else:
            data = {}
        data.update({
            'status': 'running',
            'step': step,
            'progress': progress,
            'chunk': chunk,
            'total_chunks': total,
            'device': device,
        })
        with open(status_file, 'w') as f:
            json.dump(data, f)
            f.flush()
    except Exception:
        pass


def _get_windowing_array(window_size: int, fade_size: int) -> torch.Tensor:
    fadein = torch.linspace(0, 1, fade_size)
    fadeout = torch.linspace(1, 0, fade_size)
    window = torch.ones(window_size)
    window[-fade_size:] = fadeout
    window[:fade_size] = fadein
    return window


def _normalize_audio(audio: np.ndarray) -> Tuple[np.ndarray, Dict[str, float]]:
    mono = audio.mean(0)
    mean, std = mono.mean(), mono.std()
    if std == 0:
        return audio, {'mean': mean, 'std': 1.0}
    return (audio - mean) / std, {'mean': mean, 'std': std}


def _denormalize_audio(audio: np.ndarray, norm_params: Dict[str, float]) -> np.ndarray:
    return audio * norm_params['std'] + norm_params['mean']


def _demix(
    mix: np.ndarray,
    model: nn.Module,
    config: dict,
    device: torch.device,
    progress_file: Optional[str] = None,
    pipeline_status: Optional[str] = None,
) -> Dict[str, np.ndarray]:
    """Run SCNet inference with overlap-add chunking."""
    mix_tensor = torch.tensor(mix, dtype=torch.float32)

    chunk_size = config.get('inference', {}).get('chunk_size')
    if chunk_size is None:
        chunk_size = config['audio']['chunk_size']
    num_overlap = config['inference']['num_overlap']
    batch_size = config['inference']['batch_size']
    do_normalize = config.get('inference', {}).get('normalize', False)

    fade_size = chunk_size // 10
    step = chunk_size // num_overlap
    border = chunk_size - step
    length_init = mix_tensor.shape[-1]
    window = _get_windowing_array(chunk_size, fade_size).to(device)

    norm_params = None
    if do_normalize:
        mix_np, norm_params = _normalize_audio(mix)
        mix_tensor = torch.tensor(mix_np, dtype=torch.float32)

    if length_init > 2 * border and border > 0:
        mix_tensor = F.pad(mix_tensor, (border, border), mode='reflect')

    instruments = config['training'].get('instruments') or model.sources
    num_instruments = len(instruments)

    req_shape = (num_instruments,) + tuple(mix_tensor.shape)
    result = torch.zeros(req_shape, dtype=torch.float32)
    counter = torch.zeros(req_shape, dtype=torch.float32)

    i = 0
    batch_data: List[torch.Tensor] = []
    batch_locations: List[Tuple[int, int]] = []
    chunk_idx = 0
    total = int(np.ceil(mix_tensor.shape[1] / step))

    _write_progress(progress_file, 0, total)
    _write_pipeline_status(pipeline_status, 'scnet', 0.0, 0, total, str(device))

    model.eval()
    with torch.inference_mode():
        while i < mix_tensor.shape[1]:
            part = mix_tensor[:, i:i + chunk_size].to(device)
            chunk_len = part.shape[-1]
            pad_mode = 'reflect' if chunk_len > chunk_size // 2 else 'constant'
            part = F.pad(part, (0, chunk_size - chunk_len), mode=pad_mode, value=0)

            batch_data.append(part)
            batch_locations.append((i, chunk_len))
            i += step

            if len(batch_data) >= batch_size or i >= mix_tensor.shape[1]:
                arr = torch.stack(batch_data, dim=0)
                x = model(arr)

                win = window.clone()
                if i - step == 0:
                    win[:fade_size] = 1.0
                if i >= mix_tensor.shape[1]:
                    win[-fade_size:] = 1.0

                processed = len(batch_locations)
                for j, (start, seg_len) in enumerate(batch_locations):
                    result[..., start:start + seg_len] += x[j, ..., :seg_len].cpu() * win[..., :seg_len]
                    counter[..., start:start + seg_len] += win[..., :seg_len]

                batch_data.clear()
                batch_locations.clear()

                chunk_idx += processed
                if chunk_idx % max(total // 10, 1) == 0 or chunk_idx == total:
                    print(f'  {chunk_idx}/{total} chunks...')
                _write_progress(progress_file, chunk_idx, total)
                _write_pipeline_status(
                    pipeline_status, 'scnet',
                    chunk_idx / total if total > 0 else 0.0,
                    chunk_idx, total, str(device)
                )

    estimated = result / (counter + 1e-8)
    estimated = estimated.cpu().numpy()
    np.nan_to_num(estimated, copy=False, nan=0.0)

    if length_init > 2 * border and border > 0:
        estimated = estimated[..., border:-border]

    if do_normalize and norm_params is not None:
        estimated = _denormalize_audio(estimated, norm_params)

    return {name.lower(): estimated[i] for i, name in enumerate(instruments)}


def run_scnet(args):
    """Run SCNet separation from CLI args.

    Expected args attributes:
      - model: path to the .ckpt checkpoint (or directory containing it)
      - config: optional explicit YAML config path
      - input: input audio path
      - output: output directory
      - device: "cuda" or "cpu" (default cuda)
      - progress_file: optional per-chunk progress JSON path
      - pipeline_status: optional pipeline_status.json path
    """
    warnings.filterwarnings('ignore')

    model_path = args.model
    if os.path.isdir(model_path):
        ckpts = sorted([f for f in os.listdir(model_path) if f.endswith('.ckpt')])
        if not ckpts:
            print(f'ERROR: No .ckpt found in {model_path}')
            sys.exit(1)
        model_dir = model_path
        ckpt_name = ckpts[0]
        model_path = os.path.join(model_dir, ckpt_name)
    else:
        model_dir = os.path.dirname(model_path)
        ckpt_name = os.path.basename(model_path)

    if not os.path.isfile(model_path):
        print(f'ERROR: Model not found: {model_path}')
        sys.exit(1)

    config_path = getattr(args, 'config', None)
    if not config_path:
        candidates = sorted(glob.glob(os.path.join(model_dir, '*.yaml')))
        if candidates:
            config_path = candidates[0]
    if not config_path or not os.path.isfile(config_path):
        print(f'ERROR: No SCNet YAML config found for {ckpt_name}')
        sys.exit(1)

    config = _load_config(config_path)
    device = torch.device(args.device if torch.cuda.is_available() else 'cpu')

    print('🎛️  onda scnet — SCNet')
    print(f'   Model: {ckpt_name}')
    print(f'   Config: {os.path.basename(config_path)}')
    print(f'   Device: {device}')

    model = SCNet(**config['model'])
    ckpt = torch.load(model_path, map_location='cpu', weights_only=False)
    if isinstance(ckpt, dict):
        for key in ('state', 'state_dict', 'model_state_dict', 'model', 'ema_model'):
            if key in ckpt and isinstance(ckpt[key], dict):
                ckpt = ckpt[key]
                break
    model.load_state_dict(ckpt)
    model = model.to(device).eval()

    audio, sr = _prepare_mix(args.input)
    print(f'   Audio: {audio.shape[1] / sr:.1f}s, {audio.shape[1]} samples')

    sources = _demix(
        audio, model, config, device,
        progress_file=getattr(args, 'progress_file', None),
        pipeline_status=getattr(args, 'pipeline_status', None),
    )

    os.makedirs(args.output, exist_ok=True)
    basename = os.path.splitext(os.path.basename(args.input))[0]

    # Write all source stems
    for name, stem in sources.items():
        if stem.shape[0] == 1:
            stem = np.repeat(stem, 2, axis=0)
        out = os.path.join(args.output, f'{basename}_{name}.wav')
        sf.write(out, stem.T, sr)
        print(f'   ✓ {out}')

    # Always provide vocals and instrumental for the vocal pipeline step
    if 'vocals' in sources:
        inst = None
        for name, stem in sources.items():
            if name != 'vocals':
                inst = stem if inst is None else inst + stem
        if inst is not None:
            if inst.shape[0] == 1:
                inst = np.repeat(inst, 2, axis=0)
            out = os.path.join(args.output, f'{basename}_instrumental.wav')
            sf.write(out, inst.T, sr)
            print(f'   ✓ {out} (sum of non-vocal stems)')

    print(f'✅ Done! Output in {args.output}/')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Headless SCNet source separation.')
    parser.add_argument('model', help='Model directory or .ckpt path')
    parser.add_argument('input', help='Input audio file')
    parser.add_argument('output', nargs='?', default='output_scnet', help='Output directory')
    parser.add_argument('--device', default='cuda', choices=['cuda', 'cpu'], help='Device (default: cuda)')
    parser.add_argument('--config', help='Explicit YAML config path')
    parser.add_argument('--progress-file', help='Per-chunk progress JSON file')
    parser.add_argument('--pipeline-status', help='pipeline_status.json file')
    _args = parser.parse_args()
    run_scnet(_args)
