import React from "react";
import { AbsoluteFill } from "remotion";
import { FootageAct, type BeatShot } from "../components/FootageAct";
import { Callout } from "../components/Callout";
import { C } from "../theme";

/** LIVE — the System page, "Is it still working?".
 *  Source coordinates from the recording at t=101.5s (verified on a grid).
 *
 *  Note on honesty: the fill-reporting panels are empty in this recording
 *  because nothing had settled. The film says exactly that rather than
 *  implying otherwise — the chain numbers are the ones that are real here. */

const shots: BeatShot[] = [
  // "It is reading the live chain. One hundred millisecond blocks."
  {
    id: "a10_01",
    shot: {
      srcIn: 101.0, srcOut: 102.3,
      from: { x: 218, y: 386, w: 1190, h: 580 },
      to: { x: 225, y: 379, w: 1170, h: 570 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 232, y: 660, w: 152, h: 58 }}
          appearAt={1.2} color={C.take}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 1129, y: 660, w: 214, h: 46 }}
          appearAt={2.6} color={C.model}
        />
      </>
    ),
  },

  // "And the panels that report fills say nothing has settled yet, because
  //  nothing has. It shows only what it recorded."
  {
    id: "a10_02",
    shot: {
      srcIn: 102.3, srcOut: 104.2,
      from: { x: 222, y: 185, w: 1040, h: 507 },
      to: { x: 228, y: 192, w: 1010, h: 492 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 230, y: 262, w: 552, h: 30 }}
          label="NOTHING HAS SETTLED YET" side="right" appearAt={1.8} color={C.refuse}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 230, y: 474, w: 500, h: 30 }}
          label="NO EXPOSURE RECORDED" side="right" appearAt={3.9} color={C.refuse}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 228, y: 560, w: 1010, h: 60 }}
          label="IT REPORTS ONLY WHAT IT RECORDED"
          side="bottom" appearAt={5.3} color={C.model} bare
        />
      </>
    ),
  },
];

export const Live: React.FC = () => (
  <AbsoluteFill>
    <FootageAct actName="LIVE" shots={shots} />
  </AbsoluteFill>
);
