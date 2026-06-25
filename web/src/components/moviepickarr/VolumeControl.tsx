import { PlayIcon, SquareIcon, Volume1Icon, Volume2Icon, VolumeXIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useAudio } from "@/components/audio-context";

import { isJinglePlaying, onJingleChange, playPickJingle, stopPickJingle } from "@/lib/sound";

/**
 * Pick-sound control: a speaker button that opens a small popover holding a mute
 * toggle, a volume slider, and a play/stop button to audition the pick jingle.
 * Both the on/off state and the 0..1 volume persist to localStorage (see
 * sound.ts). Closes on Escape / outside click, mirroring the date-range popover
 * pattern.
 */
export function VolumeControl() {
  const { soundEnabled, toggleSound, volume, setVolume } = useAudio();
  const [open, setOpen] = useState(false);
  const [jinglePlaying, setJinglePlaying] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  // Track jingle playback so the popover button flips between play and stop.
  useEffect(() => {
    setJinglePlaying(isJinglePlaying());
    return onJingleChange(setJinglePlaying);
  }, []);

  useEffect(() => {
    if (!open) return;
    const onPointer = (e: PointerEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const pct = Math.round(volume * 100);
  // Reflect the audible state: silent (muted or 0) → X, quiet → one wave, loud → two.
  const LevelIcon = !soundEnabled || volume === 0 ? VolumeXIcon : volume <= 0.5 ? Volume1Icon : Volume2Icon;

  return (
    <div className="volume" ref={rootRef}>
      <button
        type="button"
        className="iconbtn"
        onClick={() => setOpen((o) => !o)}
        aria-label="Pick sound settings"
        aria-expanded={open}
        title="Pick sound"
      >
        <LevelIcon />
      </button>

      {open && (
        <div className="volume__pop" role="group" aria-label="Pick sound">
          <button
            type="button"
            className="iconbtn"
            onClick={toggleSound}
            aria-label={soundEnabled ? "Mute pick sound" : "Unmute pick sound"}
            aria-pressed={!soundEnabled}
            title={soundEnabled ? "Mute" : "Unmute"}
          >
            {soundEnabled ? <Volume2Icon /> : <VolumeXIcon />}
          </button>
          <input
            className="volume__range"
            type="range"
            min={0}
            max={100}
            step={1}
            value={pct}
            onChange={(e) => setVolume(Number(e.target.value) / 100)}
            aria-label="Pick sound volume"
            aria-valuetext={`${pct}%`}
          />
          <span className="volume__pct">{pct}%</span>
          {/* Play/stop the pick jingle so you can hear it without waiting for a pick. */}
          <button
            type="button"
            className="iconbtn"
            onClick={() => (jinglePlaying ? stopPickJingle() : playPickJingle())}
            aria-label={jinglePlaying ? "Stop jingle" : "Play jingle"}
            title={jinglePlaying ? "Stop" : "Play jingle"}
          >
            {jinglePlaying ? <SquareIcon /> : <PlayIcon />}
          </button>
        </div>
      )}
    </div>
  );
}
