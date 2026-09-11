import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { C, F } from "../theme";
import { Backdrop } from "../components/Backdrop";
import { In, Node, Flow } from "../components/Kit";
import { act, byId, FPS } from "../timeline";

/** ARCHITECTURE — docs/ARCHITECTURE.md and the README diagram.
 *  price feed -> spot poller -> supervisor -> one goroutine per live window
 *  -> ONE executor (one signer, one nonce) -> Somnia chain 50312. */

const A = act("ARCHITECTURE");
const L = (id: string) => byId(id).start - A.start;
const Lend = (id: string) => byId(id).end - A.start;
const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

/** Diagram geometry, centred in the 1920x1080 frame. */
const CX = 960;
const Y = { feed: 168, poller: 288, sup: 408, loops: 560, exec: 712, chain: 848 };
const LOOP_X = [640, 960, 1280];

export const Architecture: React.FC = () => {
  const f = useCurrentFrame();
  const t = f / FPS;

  const b1 = L("a8_01");
  const b2 = L("a8_02");
  const b3 = L("a8_03");
  const b4 = L("a8_04");

  // the "one writer" emphasis takes over the lower half at a8_04
  // the diagram dims first, then the three lines come up, so they never blur together
  const diagramFade = interpolate(t, [b4 - 0.8, b4 - 0.1], [1, 0.15], clampOp);
  const writerFocus = interpolate(t, [b4 - 0.05, b4 + 0.6], [0, 1], clampOp);

  return (
    <AbsoluteFill>
      <Backdrop intensity={0.7} particles={20} seed="arch" />

      {/* pure Go claim */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "flex-start", paddingTop: 54 }}>
        <In at={b1 - 0.2} until={Lend("a8_05") + 0.4} rise={12} style={{ textAlign: "center" }}>
          <div style={{ display: "flex", gap: 12, justifyContent: "center" }}>
            {["PURE GO", "NO NODE RUNTIME", "NO JS AT RUNTIME"].map((s, i) => {
              const a = interpolate(t, [b1 + 0.4 + i * 0.9, b1 + 0.8 + i * 0.9], [0, 1], clampOp);
              return (
                <span
                  key={s}
                  style={{
                    opacity: a,
                    fontFamily: F.mono,
                    fontSize: 21,
                    fontWeight: 700,
                    letterSpacing: 2.6,
                    color: i === 0 ? C.model : C.inkDim,
                    border: "1px solid " + (i === 0 ? C.model + "77" : C.panelEdge),
                    borderRadius: 20,
                    padding: "7px 18px",
                  }}
                >
                  {s}
                </span>
              );
            })}
          </div>
        </In>
      </AbsoluteFill>

      {/* the pipeline */}
      <AbsoluteFill style={{ opacity: diagramFade }}>
        {/* nodes */}
        <div style={{ position: "absolute", left: CX - 150, top: Y.feed }}>
          <Node label="PRICE FEED" sub="GraphQL · 1s index" at={b1 + 1.6} w={300} />
        </div>
        <div style={{ position: "absolute", left: CX - 150, top: Y.poller }}>
          <Node label="SPOT POLLER" sub="one shared snapshot, 2s" at={b1 + 2.6} w={300} />
        </div>
        <div style={{ position: "absolute", left: CX - 170, top: Y.sup }}>
          <Node label="SUPERVISOR" sub="spawns and reaps per window" at={b1 + 3.8} w={340} />
        </div>

        {LOOP_X.map((x, i) => (
          <div key={x} style={{ position: "absolute", left: x - 125, top: Y.loops }}>
            <Node
              label={["BTC / 5m", "ETH / 5m", "BTC / 60m"][i]}
              sub="deadline-scoped goroutine"
              at={b2 + 0.9 + i * 0.5}
              w={250}
            />
          </div>
        ))}

        <div style={{ position: "absolute", left: CX - 190, top: Y.exec }}>
          <Node label="ONE EXECUTOR" sub="one signer · one nonce sequence" at={b4 - 0.6} w={380} strong />
        </div>
        <div style={{ position: "absolute", left: CX - 165, top: Y.chain }}>
          <Node label="SOMNIA SHANNON" sub="chain 50312" at={b4 - 0.3} w={330} />
        </div>

        {/* connectors */}
        <Flow x1={CX} y1={Y.feed + 78} x2={CX} y2={Y.poller} at={b1 + 2.3} />
        <Flow x1={CX} y1={Y.poller + 78} x2={CX} y2={Y.sup} at={b1 + 3.5} />
        {LOOP_X.map((x, i) => (
          <React.Fragment key={x}>
            <Flow x1={CX} y1={Y.sup + 80} x2={CX} y2={Y.loops - 34} at={b2 + 0.6} />
            <Flow x1={CX} y1={Y.loops - 34} x2={x} y2={Y.loops - 34} at={b2 + 0.7} pulse={false} />
            <Flow x1={x} y1={Y.loops - 34} x2={x} y2={Y.loops} at={b2 + 0.8 + i * 0.5} />
            {/* intents funnel into the single executor */}
            <Flow x1={x} y1={Y.loops + 78} x2={x} y2={Y.exec - 30} at={b4 - 0.9} />
            <Flow x1={x} y1={Y.exec - 30} x2={CX} y2={Y.exec - 30} at={b4 - 0.8} pulse={false} />
          </React.Fragment>
        ))}
        <Flow x1={CX} y1={Y.exec - 30} x2={CX} y2={Y.exec} at={b4 - 0.6} />
        <Flow x1={CX} y1={Y.exec + 82} x2={CX} y2={Y.chain} at={b4 - 0.3} />

        {/* why one goroutine per window */}
        <In at={b3 - 0.2} until={b4 - 0.1} rise={10}>
          <div
            style={{
              position: "absolute",
              left: 96,
              top: Y.loops + 14,
              width: 430,
              fontFamily: F.sans,
              fontSize: 23,
              color: C.inkDim,
              lineHeight: 1.45,
              borderLeft: "2px solid " + C.model,
              paddingLeft: 18,
            }}
          >
            A market is a state machine that is{" "}
            <span style={{ color: C.ink }}>born and dies</span>.
            <div style={{ marginTop: 10, fontFamily: F.mono, fontSize: 19, color: C.inkFaint }}>
              context.WithDeadline(expiry) <span style={{ color: C.model }}>is</span> that machine
            </div>
            <div style={{ marginTop: 8, fontSize: 20 }}>
              Inventory, working orders and P&amp;L stay goroutine-local — so most concurrency bugs
              cannot exist.
            </div>
          </div>
        </In>
      </AbsoluteFill>

      {/* a8_04 / a8_05 — many decisions, one writer */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", opacity: writerFocus }}>
        <div style={{ textAlign: "center" }}>
          {[
            ["MANY DECISIONS", C.ink, 0],
            ["ONE WRITER", C.model, 0.55],
            ["ONE NONCE SEQUENCE", C.take, 1.1],
          ].map(([label, col, d], i) => {
            const a = interpolate(
              t,
              [b4 + 0.5 + (d as number), b4 + 0.95 + (d as number)],
              [0, 1],
              clampOp
            );
            return (
              <div key={label as string}>
                {i > 0 ? (
                  <div
                    style={{
                      opacity: a,
                      fontFamily: F.mono,
                      fontSize: 34,
                      color: C.inkFaint,
                      margin: "10px 0",
                    }}
                  >
                    ↓
                  </div>
                ) : null}
                <div
                  style={{
                    opacity: a,
                    transform: "translateY(" + (1 - a) * 14 + "px)",
                    fontFamily: F.sans,
                    fontSize: i === 1 ? 92 : 64,
                    fontWeight: 800,
                    letterSpacing: -1.6,
                    color: col as string,
                    textShadow: i === 1 ? "0 0 50px " + C.modelGlow : "none",
                  }}
                >
                  {label as string}
                </div>
              </div>
            );
          })}
          <div
            style={{
              marginTop: 42,
              opacity: interpolate(t, [Lend("a8_04") - 0.2, Lend("a8_04") + 0.4], [0, 1], clampOp),
              fontFamily: F.sans,
              fontSize: 27,
              color: C.inkDim,
            }}
          >
            N goroutines may <span style={{ color: C.ink }}>decide</span> concurrently. Only one may{" "}
            <span style={{ color: C.model }}>write</span>.
          </div>
        </div>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};
