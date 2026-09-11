import asyncio, json, os, sys
import edge_tts

ROOT = os.path.dirname(os.path.abspath(__file__))
OUT  = os.path.join(ROOT, "public", "vo")
CFG  = json.load(open(os.path.join(ROOT, "script.json"), encoding="utf-8"))

async def one(beat, voice, rate, pitch):
    path = os.path.join(OUT, beat["id"] + ".mp3")
    comm = edge_tts.Communicate(beat["text"], voice, rate=rate, pitch=pitch,
                               boundary="WordBoundary")
    words, audio = [], bytearray()
    async for ch in comm.stream():
        if ch["type"] == "audio":
            audio.extend(ch["data"])
        elif ch["type"] == "WordBoundary":
            words.append({
                "w": ch["text"],
                "s": ch["offset"] / 1e7,
                "e": (ch["offset"] + ch["duration"]) / 1e7,
            })
    with open(path, "wb") as f:
        f.write(audio)
    dur = words[-1]["e"] if words else 0.0
    return {"id": beat["id"], "act": beat["act"], "text": beat["text"],
            "dur": round(dur, 3), "words": words}

async def main():
    os.makedirs(OUT, exist_ok=True)
    voice, rate, pitch = CFG["voice"], CFG["rate"], CFG.get("pitch", "+0Hz")
    res = []
    for b in CFG["beats"]:
        r = await one(b, voice, rate, pitch)
        res.append(r)
        print(f"{r['id']:9s} {r['dur']:6.2f}s  {r['text'][:60]}")
    json.dump(res, open(os.path.join(ROOT, "src", "vo-manifest.json"), "w", encoding="utf-8"), indent=1)
    total = sum(r["dur"] for r in res)
    print(f"\nTOTAL SPEECH {total:.1f}s  ({total/60:.2f} min) across {len(res)} beats")

asyncio.run(main())
