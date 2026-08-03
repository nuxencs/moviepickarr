import { MonitorIcon, SmartphoneIcon, TabletIcon } from "lucide-react";

import { sessionMeta } from "@/components/moviepickarr/account/sessions";

import type { SessionSummary } from "@/types/Response";

/** The device family behind a label, for the row icon. The label is server-side
 *  copy ("Safari on iPhone"), so matching on it keeps the shape rule in one
 *  place rather than shipping a second device field just to pick a glyph. */
function DeviceIcon({ device }: { device: string }) {
  if (device.includes("iPad")) return <TabletIcon />;
  if (device.includes("iPhone") || device.includes("Android")) return <SmartphoneIcon />;
  return <MonitorIcon />;
}

interface SessionListProps {
  sessions: SessionSummary[];
  /** Which row is mid-revoke, so only that button reports pending. */
  revokingID: number | null;
  onRevoke: (session: SessionSummary) => void;
}

/**
 * The member's own signed-in devices, most recently active first. The row for
 * the device making the request is marked and offers no sign-out: ending it is
 * what Log out is for, and a Sign out button that logs you out of the page you
 * are reading would read as a bug.
 */
export function SessionList({ sessions, revokingID, onRevoke }: SessionListProps) {
  return (
    <ul className="acc-sessions">
      {sessions.map((s) => (
        <li key={s.id} className="acc__row">
          <span className="acc__rowicon">
            <DeviceIcon device={s.device} />
          </span>
          <div className="acc__rowtext">
            <div className="acc__rowtitle">{s.device}</div>
            <div className="acc__rowmeta">{sessionMeta(s)}</div>
          </div>
          {s.current ? (
            <span className="acc-tag">This device</span>
          ) : (
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              onClick={() => onRevoke(s)}
              disabled={revokingID === s.id}
            >
              Sign out
            </button>
          )}
        </li>
      ))}
    </ul>
  );
}
