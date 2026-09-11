"""Original cinematic-electronic score for the Firstbid demo.

Synthesised from scratch: no samples, no third-party audio, fully license-clean.
Aeolian on A. Slow harmonic movement, detuned pads, sub drone, sparse pulse.

All filtering and the convolution reverb run in the frequency domain, so the
whole 5-minute bed renders in seconds rather than hours.
"""
import numpy as np
import wave
import os

SR = 48000
DUR = 310.0                       # a little longer than the cut
N = int(SR * DUR)
t = np.arange(N) / SR
rng = np.random.default_rng(7)


# ---------------------------------------------------------------- helpers
def _next_fast(n):
    p = 1
    while p < n:
        p *= 2
    return p


def fft_lowpass(x, cutoff, order=1):
    """One-pole-style lowpass applied as a zero-phase magnitude curve."""
    n = _next_fast(x.size)
    X = np.fft.rfft(x, n)
    fr = np.fft.rfftfreq(n, 1.0 / SR)
    H = 1.0 / np.sqrt(1.0 + (fr / cutoff) ** (2 * order))
    return np.fft.irfft(X * H, n)[: x.size]


def fft_convolve(x, ir):
    """Fast convolution; returns the head, same length as x."""
    n = _next_fast(x.size + ir.size)
    y = np.fft.irfft(np.fft.rfft(x, n) * np.fft.rfft(ir, n), n)
    return y[: x.size]


def smoothstep(x):
    x = np.clip(x, 0.0, 1.0)
    return x * x * (3.0 - 2.0 * x)


def env(points):
    """Piecewise-linear automation from [(sec, value), ...], smoothstepped."""
    pts = sorted(points)
    out = np.empty(N)
    out[: int(pts[0][0] * SR)] = pts[0][1]
    for i in range(len(pts) - 1):
        (t0, v0), (t1, v1) = pts[i], pts[i + 1]
        i0, i1 = int(t0 * SR), min(int(t1 * SR), N)
        if i1 <= i0:
            continue
        out[i0:i1] = v0 + (v1 - v0) * smoothstep(np.linspace(0, 1, i1 - i0))
    out[int(pts[-1][0] * SR):] = pts[-1][1]
    return out


# ---------------------------------------------------------------- harmony
# Am - F - C - G, one chord every 13.33s (eight bars at 72bpm)
A2, C3, D3, E3, F3, G3, A3 = 110.0, 130.81, 146.83, 164.81, 174.61, 196.0, 220.0
PROG = [
    (A2, [A3, C3 * 2, E3 * 2]),            # Am
    (F3 / 2, [F3, A3, C3 * 2]),            # F
    (C3 / 2, [C3 * 2, E3 * 2, G3 * 2]),    # C
    (G3 / 2, [G3, D3 * 2, G3 * 2]),        # G
]
CHORD = 13.3333


def pad():
    """Detuned three-voice pad, long attack, long release."""
    out = np.zeros(N)
    k, ts = 0, 0.0
    while ts < DUR:
        _, notes = PROG[k % len(PROG)]
        i0 = int(ts * SR)
        i1 = min(int((ts + CHORD + 4.0) * SR), N)
        if i1 <= i0:
            break
        seg = np.arange(i1 - i0) / SR
        e = smoothstep(seg / 3.5) * np.clip(1.0 - smoothstep((seg - (CHORD - 1.0)) / 4.5), 0.0, 1.0)
        v = np.zeros(seg.size)
        for nf in notes:
            for det in (-0.14, 0.0, 0.13):
                ph = 2 * np.pi * (nf + det) * seg + rng.uniform(0, 6.28)
                v += np.sin(ph) + 0.22 * np.sin(2 * ph) + 0.07 * np.sin(3 * ph)
        v /= len(notes) * 3.0
        v *= 1.0 + 0.06 * np.sin(2 * np.pi * 0.07 * seg)   # breathe
        out[i0:i1] += v * e
        ts += CHORD
        k += 1
    return out


def drone():
    """Sub octave that holds the room together."""
    out = np.zeros(N)
    k, ts = 0, 0.0
    while ts < DUR:
        root, _ = PROG[k % len(PROG)]
        i0, i1 = int(ts * SR), min(int((ts + CHORD + 2.0) * SR), N)
        if i1 <= i0:
            break
        seg = np.arange(i1 - i0) / SR
        e = smoothstep(seg / 2.0) * np.clip(1.0 - smoothstep((seg - (CHORD - 0.5)) / 2.5), 0.0, 1.0)
        f = root / 2.0
        v = 0.9 * np.sin(2 * np.pi * f * seg) + 0.18 * np.sin(2 * np.pi * 2 * f * seg)
        out[i0:i1] += v * e
        ts += CHORD
        k += 1
    return out


def pulse():
    """Sparse filtered sine tick — a heartbeat, not a beat."""
    out = np.zeros(N)
    step = 60.0 / 72.0 * 2.0          # every half note
    n, ts = 0, 0.0
    L = int(0.55 * SR)
    while ts < DUR:
        i0 = int(ts * SR)
        i1 = min(i0 + L, N)
        if i1 <= i0:
            break
        seg = np.arange(i1 - i0) / SR
        f = 74.0 if n % 4 == 0 else 58.0
        v = np.sin(2 * np.pi * f * seg) * np.exp(-seg * 11.0)
        v += 0.16 * rng.normal(0, 1, seg.size) * np.exp(-seg * 48.0)
        out[i0:i1] += v * (1.0 if n % 4 == 0 else 0.52)
        ts += step
        n += 1
    return out


def air():
    """Filtered noise bed for depth."""
    nz = fft_lowpass(rng.normal(0, 1, N), 1100.0, order=2)
    return nz * (1.0 + 0.4 * np.sin(2 * np.pi * 0.035 * t))


def reverb(x, decay=2.4, mix=0.30):
    """Convolution reverb from a decaying filtered-noise impulse response."""
    L = int(decay * SR)
    ir = rng.normal(0, 1, L) * np.exp(-np.arange(L) / (decay * 0.34 * SR))
    ir = fft_lowpass(ir, 2600.0)
    ir[: int(0.012 * SR)] = 0.0                      # pre-delay
    ir /= np.abs(ir).sum() / 18.0
    return (1 - mix) * x + mix * fft_convolve(x, ir)


print("pad...")
P = pad()
print("drone...")
D = drone()
print("pulse...")
U = pulse()
print("air...")
A = air()

# ------------------------------------------------- section dynamics, by act
# Stronger at the title and the architecture build, low under dense narration,
# a lift into the closing lines, then out. (See timeline.ts for act starts.)
pad_lvl = env([(0, 0.0), (2, 0.55), (34, 0.78), (40, 0.38), (96, 0.34), (150, 0.40),
               (186, 0.36), (210, 0.42), (228, 0.64), (250, 0.50), (262, 0.44),
               (270, 0.64), (288, 0.58), (296, 0.30), (300, 0.0), (310, 0.0)])
dro_lvl = env([(0, 0.0), (3, 0.50), (36, 0.56), (42, 0.34), (150, 0.34), (210, 0.46),
               (232, 0.60), (262, 0.40), (270, 0.58), (290, 0.42), (299, 0.0), (310, 0.0)])
pul_lvl = env([(0, 0.0), (6, 0.30), (33, 0.44), (40, 0.10), (96, 0.08), (186, 0.16),
               (210, 0.30), (230, 0.46), (252, 0.20), (262, 0.12), (270, 0.28),
               (288, 0.10), (296, 0.0), (310, 0.0)])
air_lvl = env([(0, 0.0), (4, 0.030), (40, 0.016), (210, 0.022), (270, 0.026),
               (298, 0.0), (310, 0.0)])

mix = P * pad_lvl * 0.42 + D * dro_lvl * 0.50 + U * pul_lvl * 0.30 + A * air_lvl

print("reverb...")
mix = reverb(mix, decay=2.4, mix=0.30)

# keep it out of the way of consonants
mix = fft_lowpass(mix, 5200.0, order=2)

# soft-knee limiter
mix = np.tanh(mix * 1.25) * 0.86
mix /= np.abs(mix).max() + 1e-9
mix *= 0.72

# subtle stereo: haas plus a decorrelated tail
d1, d2 = int(0.010 * SR), int(0.021 * SR)
Lch = mix.copy()
Rch = 0.90 * np.concatenate([np.zeros(d1), mix[:-d1]]) + \
      0.10 * np.concatenate([np.zeros(d2), mix[:-d2]])
st = np.stack([Lch, Rch], axis=1)
st /= np.abs(st).max() + 1e-9
st *= 0.74

out = os.path.join("public", "music.wav")
with wave.open(out, "wb") as w:
    w.setnchannels(2)
    w.setsampwidth(2)
    w.setframerate(SR)
    w.writeframes((st * 32767).astype("<i2").tobytes())
print("wrote", out, round(os.path.getsize(out) / 1e6, 1), "MB", DUR, "s")
