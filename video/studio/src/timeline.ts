/** SINGLE SOURCE OF TRUTH FOR TIMING.
 *
 *  Every start time in the film is derived from the *measured* length of the
 *  synthesised narration (src/vo-manifest.json, written by tts.py), so the
 *  picture can never drift from the voice. Re-run tts.py after editing
 *  script.json and the whole edit re-times itself.
 *
 *  Tune pacing with the four constants below.  */
import raw from "./vo-manifest.json";

export const FPS = 30;
export const W = 1920;
export const H = 1080;

export const INTRA_GAP = 0.24; // breath between beats inside an act
export const ACT_GAP   = 0.78; // beat between acts
export const PRE_ROLL  = 1.40; // silence before the first word
export const TAIL      = 4.60; // hold after the last word

export type Word = { w: string; s: number; e: number };
export type Beat = {
  id: string;
  act: ActName;
  text: string;
  dur: number;   // seconds of speech
  words: Word[];
  start: number; // absolute seconds
  end: number;
  from: number;  // absolute frames
  frames: number;
};

export type ActName =
  | "PROBLEM" | "TITLE" | "READ" | "REFUSAL" | "MODEL" | "EVIDENCE"
  | "AUTOPSY" | "COVERAGE" | "ARCHITECTURE" | "MECHANISM" | "LIVE" | "PITCH";

type RawBeat = { id: string; act: string; text: string; dur: number; words: Word[] };

/** Lay the beats end to end, inserting a longer gap whenever the act changes. */
const built: Beat[] = [];
let cursor = PRE_ROLL;
(raw as RawBeat[]).forEach((b, i) => {
  const prev = (raw as RawBeat[])[i - 1];
  if (prev) cursor += prev.act === b.act ? INTRA_GAP : ACT_GAP;
  const start = cursor;
  built.push({
    id: b.id,
    act: b.act as ActName,
    text: b.text,
    dur: b.dur,
    words: b.words,
    start,
    end: start + b.dur,
    from: Math.round(start * FPS),
    frames: Math.round(b.dur * FPS),
  });
  cursor = start + b.dur;
});

export const BEATS = built;
export const byId = (id: string): Beat => {
  const b = BEATS.find((x) => x.id === id);
  if (!b) throw new Error(`no beat ${id}`);
  return b;
};

/** An act spans from its first beat's start (minus half the preceding gap) to
 *  its last beat's end (plus half the following gap), so scenes butt cleanly. */
export type Act = {
  name: ActName;
  beats: Beat[];
  start: number;
  end: number;
  from: number;
  frames: number;
};

const order: ActName[] = [];
for (const b of BEATS) if (!order.includes(b.act)) order.push(b.act);

export const ACTS: Act[] = order.map((name, i) => {
  const beats = BEATS.filter((b) => b.act === name);
  const first = beats[0];
  const last = beats[beats.length - 1];
  const start = i === 0 ? 0 : first.start - ACT_GAP / 2;
  const isLast = i === order.length - 1;
  const end = isLast ? last.end + TAIL : last.end + ACT_GAP / 2;
  return {
    name,
    beats,
    start,
    end,
    from: Math.round(start * FPS),
    frames: Math.round((end - start) * FPS),
  };
});

export const act = (name: ActName): Act => {
  const a = ACTS.find((x) => x.name === name);
  if (!a) throw new Error(`no act ${name}`);
  return a;
};

export const TOTAL_SECONDS = ACTS[ACTS.length - 1].end;
export const TOTAL_FRAMES = Math.round(TOTAL_SECONDS * FPS);

/** Absolute frame -> frame relative to an act, for use inside a <Sequence>. */
export const relFrom = (a: Act, b: Beat) => b.from - a.from;
