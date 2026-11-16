#!/usr/bin/env python3
"""Pinned, offline-only Dire Wolf wire/audio interoperability. No radio/PTT I/O.
Usage: python3 scripts/fx25/interop.py /path/to/direwolf [--generate]
Build Dire Wolf's gen_packets/atest and `cargo build --example fx25_audio` first.
"""
import hashlib
import json
import pathlib
import subprocess
import sys
import tempfile
import wave

PIN = "eda1383f5fa9d8ba3cb27f99db1d2c79494404c9"
ROOT = pathlib.Path(__file__).resolve().parents[2]
FIX = ROOT / "graywolf-modem/testdata/fx25"
DW = pathlib.Path(sys.argv[1]).resolve()
GENERATE = "--generate" in sys.argv
GW = ROOT / "target/debug/examples/fx25_audio"
CAPS = [239,128,64,32,223,128,64,32,191,128,64]

def run(args, **kw):
    return subprocess.run([str(a) for a in args], check=True, capture_output=True, **kw).stdout

assert run(["git", "-C", DW, "rev-parse", "HEAD"]).decode().strip() == PIN
with tempfile.TemporaryDirectory() as tmp:
    tmp = pathlib.Path(tmp)
    helper = tmp / "reference"
    run(["cc", "-D_GNU_SOURCE", "-I"+str(DW/"src"), "-I"+str(DW/"build/src"),
         ROOT/"scripts/fx25/reference.c", *[DW/"src"/(n+".c") for n in
         ["fx25_init","fx25_send","fx25_encode","fx25_extract","fcs_calc"]], "-o", helper])
    modulator = tmp / "modulate"
    run(["cc", "-D_GNU_SOURCE", "-I"+str(DW/"src"), "-I"+str(DW/"build/src"),
         ROOT/"scripts/fx25/modulate.c", DW/"src/gen_tone.c", "-lm", "-o", modulator])
    vectors = []
    for tag, cap in enumerate(CAPS, 1):
        # Independent C generator decides the actual maximum, including FCS
        # stuffing. Payload tails include literal flags, FF and zero bytes.
        base = bytes.fromhex("82a0a4a640406096a6729c40406103f0")
        for kind, size in [("short",20),("boundary",cap//2),("maximum",cap)]:
            frame = base + bytes(max(0,size-len(base)-4)) + b"\x7e\xff\x00\x7e"
            while True:
                result = subprocess.run([helper,str(tag),frame.hex()],capture_output=True)
                if result.returncode == 0:
                    break
                assert len(frame) > 16
                frame = frame[:16]+frame[17:]
            expected = result.stdout.hex()
            actual = run([GW,"wire",frame.hex(),tag]).decode().strip()
            assert actual == expected, (tag,kind,"wire mismatch")
            vectors.append(dict(tag=tag,case=kind,frame=frame.hex(),wire=expected))
            wav = tmp / "graywolf.wav"
            run([GW,"encode",frame.hex(),wav,tag])
            # Real Dire Wolf atest enforces exactly one delivery and emits hex.
            out = run([DW/"build/src/atest","-B","1200","-h","-d","x","-L","1","-G","1",wav]).decode()
            # Hex dumps include offsets/newlines; check first/last payload by
            # full extraction below, using atest's known hex-dump layout.
            import re
            dump = re.findall(r"^\s*[0-9a-fA-F]{3}: ((?: [0-9a-fA-F]{2})+)", out, re.M)
            decoded = bytes.fromhex("".join(dump))
            assert decoded == frame, (tag,kind,"atest payload",out)
    # Exercise the exact production mode-selection/sample builder too.
    for mode in ["auto","16","32","64"]:
        v=vectors[0]
        wav=tmp/"production-mode.wav"
        run([GW,"encode-mode",v["frame"],wav,mode])
        out=run([DW/"build/src/atest","-B","1200","-h","-L","1","-G","1",wav]).decode()
        dump=re.findall(r"^\s*[0-9a-fA-F]{3}: ((?: [0-9a-fA-F]{2})+)",out,re.M)
        assert bytes.fromhex("".join(dump)).hex()==v["frame"],mode
    audio_frames = []
    wave_chunks = []
    for tag, cap in enumerate(CAPS,1):
        for kind, length in [("short",1),("boundary",max(1,cap//2-21)),("maximum",cap-23)]:
            payload = "x" * length + "~"
            text = f"KS9N>APRS:{payload}\n"
            wav = tmp / "direwolf.wav"
            out = run([DW/"build/src/gen_packets","-B","1200","-X",100+tag,"-o",wav,"-"],input=text.encode())
            assert b"Falling back" not in out
            # gen_packets deliberately retains the input newline as payload.
            expected = "82a0a4a64040e096a6729c4040e103f0" + (payload+"\n").encode().hex()
            got = run([GW,"decode",wav]).decode().splitlines()
            assert got==[expected+" fx25"], (tag,kind,got)
            audio_frames.append(dict(tag=tag,case=kind,frame=expected))
            with wave.open(str(wav),"rb") as w:
                params = w.getparams()
                wave_chunks.append(w.readframes(w.getnframes()))
    # Independent RS encoder AND actual Dire Wolf tone generator, with
    # controlled symbol errors before modulation to require FEC recovery.
    # Exactly half the parity count of data symbols are corrupted.
    damaged_chunks = []
    damaged_frames = []
    for v in vectors:
        if v["case"] != "short": continue
        tag = v["tag"]
        wire = bytearray.fromhex(v["wire"])
        parity = 16 if tag<=4 else 32 if tag<=8 else 64
        for n in range(parity//2): wire[8+n] ^= n+1
        pcm = run([modulator],input=b"\x7e"*45+wire+b"\x7e"*15)
        wav=tmp/"damaged.wav"
        with wave.open(str(wav),"wb") as w:
            w.setnchannels(1); w.setsampwidth(2); w.setframerate(48000); w.writeframes(pcm)
        got=run([GW,"decode",wav]).decode().splitlines()
        assert got==[v["frame"]+" fx25"],(tag,got)
        damaged_frames.append(dict(tag=tag,frame=v["frame"]))
        damaged_chunks.append(pcm)
    fixture = dict(direwolf_commit=PIN,wire_vectors=vectors,audio_frames=audio_frames,damaged_frames=damaged_frames)
    if GENERATE:
        FIX.mkdir(parents=True,exist_ok=True)
        (FIX/"direwolf.json").write_text(json.dumps(fixture,indent=2)+"\n")
        with wave.open(str(FIX/"direwolf.wav"),"wb") as w:
            w.setparams(params)
            w.writeframes(b"".join(wave_chunks))
        with wave.open(str(FIX/"direwolf-damaged.wav"),"wb") as w:
            w.setnchannels(1); w.setsampwidth(2); w.setframerate(48000); w.writeframes(b"".join(damaged_chunks))
        (FIX/"SHA256SUMS").write_text("".join(hashlib.sha256((FIX/n).read_bytes()).hexdigest()+"  "+n+"\n" for n in ["direwolf.json","direwolf.wav","direwolf-damaged.wav"]))
    else:
        assert json.loads((FIX/"direwolf.json").read_text()) == fixture
    print(f"PASS: {len(vectors)} exact wire vectors; {len(vectors)} Graywolf -> Dire Wolf audio + 4 production mode cases; {len(audio_frames)} Dire Wolf -> Graywolf audio; 11 symbol-damaged Dire Wolf -> Graywolf FEC recoveries")
