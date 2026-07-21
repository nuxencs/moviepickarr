import { PlayIcon, SquareIcon, Volume2Icon, VolumeXIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { useAudio } from "@/components/audio-context";

import { isJinglePlaying, onJingleChange, playDrawJingle, stopDrawJingle } from "@/lib/sound";

/**
 * Draw-sound control, rendered inline inside the profile panel's Preferences
 * section: a mute toggle, a volume slider with its percentage, and a play/stop
 * button to audition the draw jingle without waiting for a real draw. Both the
 * on/off state and the 0..1 volume persist to localStorage (see sound.ts).
 *
 * The panel is the single surface this lives on, so the control owns no
 * open/close state and no Escape / outside-click handling of its own — it just
 * presents the audio state and the jingle play/stop helpers, both unchanged.
 */
export function VolumeControl() {
  const { soundEnabled, toggleSound, volume, setVolume } = useAudio();
  const [jinglePlaying, setJinglePlaying] = useState(false);

  // Track jingle playback so the audition button flips between play and stop.
  useEffect(() => {
    setJinglePlaying(isJinglePlaying());
    return onJingleChange(setJinglePlaying);
  }, []);

  const pct = Math.round(volume * 100);

  return (
    <div className="volume" role="group" aria-label="Draw sound">
      <div className="volume__head">
        <span className="volume__label">Draw sound</span>
        <span className="volume__pct">{pct}%</span>
      </div>
      <div className="volume__row">
        <button
          type="button"
          className="iconbtn"
          onClick={toggleSound}
          aria-label={soundEnabled ? "Mute draw sound" : "Unmute draw sound"}
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
          aria-label="Draw sound volume"
          aria-valuetext={`${pct}%`}
        />
        {/* Play/stop the draw jingle so you can hear it without waiting for a draw. */}
        <button
          type="button"
          className="iconbtn"
          onClick={() => (jinglePlaying ? stopDrawJingle() : playDrawJingle())}
          aria-label={jinglePlaying ? "Stop jingle" : "Play jingle"}
          title={jinglePlaying ? "Stop" : "Play jingle"}
        >
          {jinglePlaying ? <SquareIcon /> : <PlayIcon />}
        </button>
      </div>
    </div>
  );
}
