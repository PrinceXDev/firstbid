"""Re-attach punctuation to the word timings.

edge-tts reports WordBoundary events with punctuation stripped, which makes the
captions read as a run-on. This walks the original script text alongside the
reported words and restores each token's real spelling, so subtitles carry the
commas and full stops the voice is actually performing.

Only src/vo-manifest.json is rewritten; the audio is untouched.
"""
import json
import os
import re

ROOT = os.path.dirname(os.path.abspath(__file__))
script = json.load(open(os.path.join(ROOT, "script.json"), encoding="utf-8"))
manifest = json.load(open(os.path.join(ROOT, "src", "vo-manifest.json"), encoding="utf-8"))

TEXTS = {b["id"]: b["text"] for b in script["beats"]}
strip = lambda s: re.sub(r"[^a-z0-9]", "", s.lower())

fixed = 0
for beat in manifest:
    tokens = TEXTS[beat["id"]].split()
    ti = 0
    for w in beat["words"]:
        target = strip(w["w"])
        if not target:
            continue
        # advance through the source tokens until the letters line up
        for probe in range(ti, min(ti + 4, len(tokens))):
            if strip(tokens[probe]) == target:
                w["w"] = tokens[probe]
                ti = probe + 1
                fixed += 1
                break

json.dump(manifest, open(os.path.join(ROOT, "src", "vo-manifest.json"), "w", encoding="utf-8"),
          indent=1, ensure_ascii=False)
print("restored punctuation on", fixed, "words across", len(manifest), "beats")
