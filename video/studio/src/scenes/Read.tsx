import React from "react";
import { AbsoluteFill } from "remotion";
import { FootageAct, type BeatShot } from "../components/FootageAct";
import { Callout } from "../components/Callout";
import { C } from "../theme";

/** THE READ — the live product, doing its job.
 *  Source coordinates are from the recording at t=2s / t=7s (page at top),
 *  verified against a 100px reference grid. */

const HERO_WIDE = { x: 200, y: 180, w: 1520, h: 741 };
const MODEL_NUM = { x: 200, y: 232, w: 760, h: 370 };
const MKT_MODEL = { x: 400, y: 250, w: 1290, h: 629 };
const OUR_READ = { x: 600, y: 300, w: 1120, h: 546 };
const FIELD_WIDE = { x: 190, y: 430, w: 1540, h: 751 };

const shots: BeatShot[] = [
  // "This is it, running against the live Shannon testnet book."
  //  Framed to include the nav, so the product's depth reads immediately.
  {
    id: "a2_01",
    shot: {
      srcIn: 7.0, srcOut: 8.4,
      from: { x: 140, y: 92, w: 1680, h: 819 },
      to: { x: 150, y: 96, w: 1650, h: 804 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 508, y: 104, w: 448, h: 34 }}
          label="SIX VIEWS · ONE ENGINE" side="bottom" appearAt={0.9} holdUntil={2.1} bare
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 225, y: 196, w: 200, h: 36 }}
          label="ETH / 5m" note="one live window" side="right" appearAt={2.5}
        />
      </>
    ),
  },

  // "One window. Ethereum, five minutes. The model's probability of Up: 0.812."
  {
    id: "a2_02",
    shot: { srcIn: 1.3, srcOut: 2.9, from: MODEL_NUM, to: { x: 196, y: 228, w: 840, h: 410 } },
    overlay: ({ shot, dur }) => (
      <>
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 228, y: 296, w: 190, h: 66 }}
          label="FAIR VALUE" note="the model's own probability of Up"
          side="right" appearAt={2.0}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 228, y: 382, w: 285, h: 26 }}
          label="± 0.093" note="its own stated uncertainty"
          side="right" appearAt={4.4}
        />
      </>
    ),
  },

  // "But the market's mid is 0.942, against the model's 0.812..."
  {
    id: "a2_04",
    // wide enough to hold the model's number and the book's mid in one frame,
    // which is the comparison the narration is making
    shot: { srcIn: 7.0, srcOut: 8.7, from: { x: 196, y: 240, w: 1520, h: 741 },
            to: { x: 204, y: 248, w: 1490, h: 726 } },
    overlay: ({ shot, dur }) => (
      <>
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 428, y: 444, w: 130, h: 32 }}
          label="THE BOOK" side="bottom" appearAt={1.2} color={C.book}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 640, y: 296, w: 1050, h: 104 }}
          label="MARKET VS MODEL" side="bottom" appearAt={3.6}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 1170, y: 318, w: 370, h: 44 }}
          label="THE GAP" note="the book is more certain than the evidence"
          side="top" appearAt={5.4} color={C.warn}
        />
      </>
    ),
  },

  // "So the read is: take the bid. Down is cheap."
  {
    id: "a2_05",
    shot: { srcIn: 7.6, srcOut: 8.5, from: OUR_READ, to: { x: 615, y: 308, w: 1090, h: 531 } },
    overlay: ({ shot, dur }) => (
      <Callout
        shot={shot} durationInFrames={dur}
        box={{ x: 652, y: 448, w: 404, h: 40 }}
        label="TAKE" note="bid is above fair value" side="bottom"
        appearAt={0.7} color={C.take}
      />
    ),
  },

  // "Every live window, ranked by disagreement over noise. Not by volume."
  {
    id: "a2_06",
    shot: { srcIn: 12.2, srcOut: 17.6, from: { x: 190, y: 258, w: 1540, h: 751 },
            to: { x: 190, y: 277, w: 1540, h: 751 } },
    overlay: ({ shot, dur }) => (
      <Callout
        shot={shot} durationInFrames={dur}
        box={{ x: 1320, y: 606, w: 386, h: 32 }}
        side="left" appearAt={1.6}
      />
    ),
  },
];

export const Read: React.FC = () => (
  <AbsoluteFill>
    <FootageAct actName="READ" shots={shots} />
  </AbsoluteFill>
);
