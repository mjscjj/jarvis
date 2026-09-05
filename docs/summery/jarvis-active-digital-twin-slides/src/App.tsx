import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { deckRegistry, type SceneDefinition } from "./deck";

const STAGE_WIDTH = 1920;
const STAGE_HEIGHT = 1080;

declare global {
  interface Window { __DECK_REGISTRY__: Array<Pick<SceneDefinition, "id" | "title" | "section" | "beats" | "profile" | "theme">>; }
}

window.__DECK_REGISTRY__ = deckRegistry.map(({ id, title, section, beats, profile, theme }) => ({ id, title, section, beats, profile, theme }));

function useStageScale() {
  const viewportRef = useRef<HTMLDivElement>(null);
  const [scale, setScale] = useState(1);
  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const update = () => {
      const visualWidth = window.visualViewport?.width ?? window.innerWidth;
      const visualHeight = window.visualViewport?.height ?? window.innerHeight;
      const width = Math.min(viewport.clientWidth, visualWidth);
      const height = Math.min(viewport.clientHeight, visualHeight);
      setScale(Math.max(.01, Math.min(width / STAGE_WIDTH, height / STAGE_HEIGHT)));
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(viewport);
    window.visualViewport?.addEventListener("resize", update);
    window.addEventListener("resize", update);
    return () => { observer.disconnect(); window.visualViewport?.removeEventListener("resize", update); window.removeEventListener("resize", update); };
  }, []);
  return { viewportRef, scale };
}

function readFrame() {
  const params = new URLSearchParams(window.location.search);
  const requestedId = params.get("scene") ?? deckRegistry[0].id;
  const sceneIndex = deckRegistry.findIndex((scene) => scene.id === requestedId);
  if (sceneIndex < 0) return { sceneIndex: -1, beat: 0, requestedId };
  const requestedBeat = Number(params.get("beat") ?? "0");
  const beat = Number.isFinite(requestedBeat) ? Math.min(deckRegistry[sceneIndex].beats - 1, Math.max(0, requestedBeat)) : 0;
  return { sceneIndex, beat, requestedId };
}

function Navigator({ activeIndex, goTo }: { activeIndex: number; goTo: (index: number, beat?: number) => void }) {
  return <nav className="stage-navigator" aria-label="页面导航" data-interactive onPointerDown={(event) => event.stopPropagation()} onClick={(event) => event.stopPropagation()} onWheel={(event) => { event.stopPropagation(); if (Math.abs(event.deltaY) >= 8) goTo(activeIndex + (event.deltaY > 0 ? 1 : -1)); }}>
    <div className="navigator-mask">{deckRegistry.map((scene, index) => {
      const distance = index - activeIndex;
      return <button key={scene.id} type="button" aria-label={`跳转到页面：${scene.title}`} aria-current={index === activeIndex ? "page" : undefined} className={index === activeIndex ? "active" : ""} style={{ opacity: Math.max(.16, 1 - Math.abs(distance) * .22), transform: `translateY(calc(-50% + ${distance * 44}px)) scale(${index === activeIndex ? 1 : .68})` }} onClick={() => goTo(index, 0)}><span /><small>{String(index + 1).padStart(2, "0")}</small></button>;
    })}</div>
  </nav>;
}

function Chrome({ scene, beat }: { scene: SceneDefinition; beat: number }) {
  return <><header className="stage-header"><span className="stage-brand">JARVIS <i>/</i> ACTIVE DIGITAL TWIN</span><span>{scene.section}</span></header><footer className="stage-footer"><span>状态持续更新 · 决策持续发生</span><div className="beat-progress">{Array.from({ length: scene.beats }, (_, index) => <i key={index} className={index <= beat ? "active" : ""} />)}</div><span>COMPILE REALITY → DECIDE → ACT</span></footer></>;
}

function DeckStage({ scene, activeIndex, beat, print = false, goTo }: { scene: SceneDefinition; activeIndex: number; beat: number; print?: boolean; goTo?: (index: number, beat?: number) => void }) {
  return <section className={`stage deck-stage theme-${scene.theme} ${print ? "print-stage snapshot" : ""}`} data-scene-id={scene.id} data-beat={beat} data-beats={scene.beats} data-profile={scene.profile} data-theme={scene.theme} aria-label={scene.title}><div className="stage-ambient" /><Chrome scene={scene} beat={beat} /><main className="scene-canvas" key={scene.id}>{scene.render(beat)}</main>{!print && goTo && <Navigator activeIndex={activeIndex} goTo={goTo} />}</section>;
}

function RouteError({ requestedId }: { requestedId: string }) {
  return <section className="stage route-error" data-route-error={requestedId}><p>FRAME ROUTE ERROR</p><h1>找不到场景：{requestedId}</h1><span>请使用 registry 中的稳定 scene id。</span></section>;
}

function PrintDeck() {
  return <div className="print-deck" data-print-deck>{deckRegistry.map((scene, index) => <div className="print-page" key={scene.id}><div className="print-transform"><DeckStage scene={scene} activeIndex={index} beat={scene.beats - 1} print /></div></div>)}</div>;
}

export default function App() {
  const params = new URLSearchParams(window.location.search);
  const print = params.get("print") === "1";
  const snapshot = print || params.get("snapshot") === "1";
  const initial = useMemo(readFrame, []);
  const [sceneIndex, setSceneIndex] = useState(initial.sceneIndex);
  const [beat, setBeat] = useState(initial.beat);
  const [ready, setReady] = useState(false);
  const { viewportRef, scale } = useStageScale();
  const pointerStart = useRef<{ x: number; y: number } | null>(null);
  const suppressClickUntil = useRef(0);
  useEffect(() => { document.fonts.ready.then(() => setReady(true)); }, []);

  const writeFrame = useCallback((nextScene: number, nextBeat = 0) => {
    const boundedScene = Math.min(deckRegistry.length - 1, Math.max(0, nextScene));
    const boundedBeat = Math.min(deckRegistry[boundedScene].beats - 1, Math.max(0, nextBeat));
    setSceneIndex(boundedScene); setBeat(boundedBeat);
    const url = new URL(window.location.href);
    url.searchParams.set("scene", deckRegistry[boundedScene].id); url.searchParams.set("beat", String(boundedBeat)); url.searchParams.delete("print");
    window.history.replaceState({}, "", url);
  }, []);
  const next = useCallback(() => { if (sceneIndex < 0) return; const scene = deckRegistry[sceneIndex]; if (beat < scene.beats - 1) writeFrame(sceneIndex, beat + 1); else if (sceneIndex < deckRegistry.length - 1) writeFrame(sceneIndex + 1, 0); }, [beat, sceneIndex, writeFrame]);
  const previous = useCallback(() => { if (sceneIndex < 0) return; if (beat > 0) writeFrame(sceneIndex, beat - 1); else if (sceneIndex > 0) writeFrame(sceneIndex - 1, deckRegistry[sceneIndex - 1].beats - 1); }, [beat, sceneIndex, writeFrame]);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.repeat) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, button, a, [contenteditable=true], [data-interactive]")) return;
      if (["ArrowRight", "ArrowDown", " "].includes(event.key)) { event.preventDefault(); next(); }
      else if (["ArrowLeft", "ArrowUp"].includes(event.key)) { event.preventDefault(); previous(); }
    };
    window.addEventListener("keydown", onKeyDown); return () => window.removeEventListener("keydown", onKeyDown);
  }, [next, previous]);
  if (print) return <PrintDeck />;
  const scene = sceneIndex >= 0 ? deckRegistry[sceneIndex] : null;
  return <div className={`stage-viewport ${snapshot ? "snapshot" : ""}`} ref={viewportRef} data-deck-ready={ready} onClick={(event) => { if (Date.now() < suppressClickUntil.current) return; if ((event.target as HTMLElement).closest("button, a, input, textarea, select, [data-interactive]")) return; next(); }} onPointerDown={(event) => { if ((event.target as HTMLElement).closest("button, a, input, textarea, select, [data-interactive]")) return; pointerStart.current = { x: event.clientX, y: event.clientY }; }} onPointerUp={(event) => { if (!pointerStart.current) return; const dx = event.clientX - pointerStart.current.x; const dy = event.clientY - pointerStart.current.y; pointerStart.current = null; if (Math.max(Math.abs(dx), Math.abs(dy)) < 70) return; suppressClickUntil.current = Date.now() + 450; if (Math.abs(dx) >= Math.abs(dy)) (dx < 0 ? next : previous)(); else (dy < 0 ? next : previous)(); }}><div className="stage-frame" style={{ width: STAGE_WIDTH * scale, height: STAGE_HEIGHT * scale }}><div className="stage-transform" style={{ width: STAGE_WIDTH, height: STAGE_HEIGHT, transform: `scale(${scale})` }}>{scene ? <DeckStage scene={scene} activeIndex={sceneIndex} beat={beat} goTo={writeFrame} /> : <RouteError requestedId={initial.requestedId} />}</div></div></div>;
}
