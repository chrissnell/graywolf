#!/usr/bin/env python3
"""Deterministic offline stress observations, not an HF performance benchmark.

Usage: GRAYWOLF_FX25_TOOL=... python3 scripts/fx25/stress300.py /pinned/direwolf
Requires interop.py's pinned Dire Wolf with gen_packets built. No audio/PTT I/O.
These deterministic cases assert one exact delivery; they are not RF benchmarks.
"""
import array
import hashlib
import json
import math
import os
import pathlib
import random
import subprocess
import sys
import tempfile
import wave

ROOT = pathlib.Path(__file__).resolve().parents[2]
DW = pathlib.Path(sys.argv[1]).resolve()
GW = pathlib.Path(os.environ.get('GRAYWOLF_FX25_TOOL', ROOT/'target/release/examples/fx25_audio'))
os.environ['GRAYWOLF_FX25_BAUD'] = '300'
PIN = 'eda1383f5fa9d8ba3cb27f99db1d2c79494404c9'
assert subprocess.check_output(['git','-C',DW,'rev-parse','HEAD'],text=True).strip() == PIN
header = bytes.fromhex('82a0a4a64040e09c6086829898ef03f0')
payload = b'>Experimental offline FX25 300 test\n'
expected = (header + payload).hex()
with tempfile.TemporaryDirectory() as tmp:
    tmp = pathlib.Path(tmp)
    clean = tmp/'clean.wav'
    subprocess.run([DW/'build/src/gen_packets','-B','300','-X','32','-o',clean,'-'],
                   input=b'N0CALL-7>APRS:'+payload,check=True,capture_output=True)
    with wave.open(str(clean),'rb') as w:
        pcm = array.array('h',w.readframes(w.getnframes()))
        rate = w.getframerate()
    rms = math.sqrt(sum(v*v for v in pcm)/len(pcm))
    variants = [('clean', pcm, rate)]
    for snr in [30,20,10,0]:
        rng = random.Random(300_000+snr)
        sigma = rms * 10**(-snr/20)
        samples = array.array('h',(max(-32768,min(32767,round(v+rng.gauss(0,sigma)))) for v in pcm))
        variants.append((f'AWGN {snr} dB whole-record RMS',samples,rate))
    # Header-rate changes model a combined clock/tone offset, not isolated fading.
    for ratio in [.99,1.01]:
        variants.append((f'clock and tone {ratio:.2f}x',pcm,round(rate*ratio)))
    rng = random.Random(300)
    variants.append(('noise-only control',array.array('h',(rng.randint(-4000,4000) for _ in range(rate*5))),rate))
    for name,samples,sample_rate in variants:
        wav = tmp/'variant.wav'
        with wave.open(str(wav),'wb') as w:
            w.setnchannels(1);w.setsampwidth(2);w.setframerate(sample_rate);w.writeframes(samples.tobytes())
        lines = subprocess.check_output([GW,'decode-detail',wav],text=True).splitlines()
        frames = [json.loads(v) for v in lines]
        valid = [f for f in frames if f['frame'] == expected]
        if name != 'noise-only control': assert len(valid)==1 and len(frames)==1, (name,frames)
        if name == 'noise-only control': assert not frames
        # Never silently accept a wrong payload in this deterministic experiment.
        assert len(valid)==len(frames), (name,frames)
        print(json.dumps(dict(case=name,sha256=hashlib.sha256(wav.read_bytes()).hexdigest(),
                              frames=len(frames),corrected=[f['corrected'] for f in valid])),flush=True)
    # Exact sample-count comparison: same frame, 300 ms delay and 100 ms tail.
    for length in [20,60,100,180,236]:
        frame = header + b'x'*max(0,length-len(header))
        for parity in [0,16,32,64]:
            measured=json.loads(subprocess.check_output([GW,'airtime',frame.hex(),str(parity)],text=True))
            print(json.dumps(dict(frame_bytes=len(frame),mode='auto' if parity==0 else str(parity),**measured)),flush=True)
