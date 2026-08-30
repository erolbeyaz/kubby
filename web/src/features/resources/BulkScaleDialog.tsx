import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { ConfirmDialog } from "@/components/ConfirmDialog";
import { ApiError, api, type ResourceRow } from "@/lib/api";

interface BulkScaleDialogProps {
  clusterId: string;
  typeKey: string;
  kind: string;
  rows: ResourceRow[];
  onChanged: () => void;
  onClose: () => void;
}

type Mode = "set" | "restore";

/**
 * Scaling a set of workloads at once, for a drill.
 *
 * Taking everything to zero is one number for every workload; bringing it back is not,
 * because they did not all run the same count and Kubernetes keeps no record of what
 * they ran. So the count each one had is written onto the object before it is changed,
 * and restoring reads it back per workload rather than asking for a number that would be
 * wrong for most of them.
 *
 * One request per workload rather than one for the set: each is its own authorisation
 * check and its own audit record, which is what a change of this size should leave
 * behind. A failure part-way is reported per name rather than losing the ones that
 * worked.
 */
export function BulkScaleDialog({
  clusterId,
  typeKey,
  kind,
  rows,
  onChanged,
  onClose,
}: BulkScaleDialogProps) {
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<Mode>("set");
  const [wanted, setWanted] = useState("0");
  const [busy, setBusy] = useState(false);
  const [failures, setFailures] = useState<string[]>([]);
  const [done, setDone] = useState(0);

  const replicas = Number(wanted);
  const wantedIsANumber = Number.isInteger(replicas) && replicas >= 0;

  const counts = rows.map((row) => currentReplicas(row));
  const recorded = rows.map((row) => recordedReplicas(row));
  const mixed = new Set(counts).size > 1;
  const restorable = recorded.filter((count) => count !== null).length;

  // Nothing to restore is not a scale of zero — it is a dialog with nothing to do.
  const valid = mode === "restore" ? restorable > 0 : wantedIsANumber;

  const run = async () => {
    setBusy(true);
    setFailures([]);
    setDone(0);

    const failed: string[] = [];
    for (const row of rows) {
      try {
        await api.scale(clusterId, {
          typeKey,
          namespace: row.namespace ?? "",
          name: row.name,
          replicas: mode === "restore" ? 0 : replicas,
          ...(mode === "restore" ? { restore: true } : {}),
        });
        setDone((current) => current + 1);
      } catch (caught) {
        failed.push(
          `${row.namespace ?? ""}/${row.name}: ${caught instanceof ApiError ? caught.message : "refused"}`,
        );
      }
    }

    void queryClient.invalidateQueries({ queryKey: ["resources"] });
    setBusy(false);

    if (failed.length > 0) {
      setFailures(failed);
      return;
    }
    onChanged();
    onClose();
  };

  return (
    <ConfirmDialog
      title={`Scale ${rows.length} ${kind}${rows.length === 1 ? "" : "s"}`}
      confirmLabel={
        mode === "restore"
          ? "Restore"
          : replicas === 0
            ? "Scale to zero"
            : "Scale"
      }
      busy={busy}
      disabled={!valid}
      destructive={mode === "set" && replicas === 0}
      {...(failures.length > 0
        ? { error: `${failures.length} of ${rows.length} were refused.` }
        : {})}
      onConfirm={() => void run()}
      onCancel={onClose}
    >
      <div className="flex flex-col gap-3 text-left">
        <div className="flex gap-1.5">
          <ModeTab
            label="Set to"
            active={mode === "set"}
            onClick={() => setMode("set")}
          />
          <ModeTab
            label="Restore previous"
            active={mode === "restore"}
            onClick={() => setMode("restore")}
          />
        </div>

        {mode === "set" ? (
          <label
            className="flex items-center gap-3"
            style={{ fontSize: "var(--text-secondary-size)" }}
          >
            <span style={{ color: "var(--text-secondary)" }}>Replicas:</span>
            <input
              value={wanted}
              onChange={(event) =>
                setWanted(event.target.value.replace(/[^0-9]/g, ""))
              }
              inputMode="numeric"
              aria-label="Replicas"
              autoFocus
              className="w-24 border-0 border-b bg-transparent px-1 py-1 text-center font-mono outline-none"
              style={{
                borderColor: "var(--accent)",
                color: "var(--text-primary)",
                fontSize: "var(--text-secondary-size)",
              }}
            />
          </label>
        ) : (
          <p
            style={{
              fontSize: "var(--text-micro)",
              color: "var(--text-muted)",
            }}
          >
            Each workload goes back to the count it ran before Kubby last scaled
            it, read from the object itself. One that Kubby has not scaled has
            nothing recorded and is marked below rather than guessed at.
          </p>
        )}

        {/* What is about to be flattened. A drill that takes 3, 5 and 2 to zero and comes
            back as 1, 1 and 1 is a worse outage than the one it was rehearsing. */}
        <div
          className="max-h-40 overflow-auto border"
          style={{
            borderRadius: "var(--radius-sharp)",
            borderColor: "var(--border-subtle)",
          }}
        >
          {rows.map((row, index) => {
            const target =
              mode === "restore"
                ? recorded[index]
                : wantedIsANumber
                  ? replicas
                  : null;
            const nothingRecorded =
              mode === "restore" && recorded[index] === null;

            return (
              <div
                key={`${row.namespace}/${row.name}`}
                className="flex items-baseline gap-2 px-2 py-1"
                style={{ fontSize: "var(--text-micro)" }}
              >
                <span
                  className="min-w-0 flex-1 truncate font-mono"
                  style={{ color: "var(--text-secondary)" }}
                >
                  {row.namespace}/{row.name}
                </span>
                <span
                  className="font-mono"
                  style={{ color: "var(--text-muted)" }}
                >
                  now {counts[index]}
                </span>
                {nothingRecorded ? (
                  <span style={{ color: "var(--status-warn)" }}>no record</span>
                ) : (
                  target !== null && (
                    <span
                      className="font-mono"
                      style={{
                        color:
                          target === 0
                            ? "var(--status-error)"
                            : "var(--accent)",
                      }}
                    >
                      &rarr; {target}
                    </span>
                  )
                )}
              </div>
            );
          })}
        </div>

        {mode === "restore" && restorable < rows.length && (
          <p
            style={{
              fontSize: "var(--text-micro)",
              color: "var(--status-warn)",
            }}
          >
            {rows.length - restorable} of {rows.length} were not scaled from
            here and will be reported as refused.
          </p>
        )}

        {mode === "set" && mixed && (
          <p
            style={{
              fontSize: "var(--text-micro)",
              color: "var(--status-warn)",
            }}
          >
            These do not all run the same count. Kubby records what each one
            had, so Restore previous brings them back individually.
          </p>
        )}

        {busy && (
          <p
            style={{
              fontSize: "var(--text-micro)",
              color: "var(--text-muted)",
            }}
          >
            {done} of {rows.length} done…
          </p>
        )}

        {failures.length > 0 && (
          <div
            className="flex flex-col gap-0.5"
            style={{ fontSize: "var(--text-micro)" }}
          >
            {failures.map((failure) => (
              <span key={failure} style={{ color: "var(--status-error)" }}>
                {failure}
              </span>
            ))}
          </div>
        )}
      </div>
    </ConfirmDialog>
  );
}

function ModeTab({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className="tool-chip"
      style={{
        borderColor: active ? "var(--accent)" : "var(--border-default)",
        color: active ? "var(--accent)" : "var(--text-secondary)",
      }}
    >
      {label}
    </button>
  );
}

/** What Kubby recorded on the object before it last scaled this workload. */
function recordedReplicas(row: ResourceRow): number | null {
  const recorded = row.fields["scaledFrom"];
  if (recorded === undefined) return null;
  const count = Number(recorded);
  return Number.isInteger(count) && count >= 0 ? count : null;
}

/** What the row says it runs now, which the list already projects. */
function currentReplicas(row: ResourceRow): number {
  const ready = row.fields["ready"] ?? "";
  const desired = ready.includes("/")
    ? Number(ready.split("/")[1])
    : Number(row.fields["desired"]);
  return Number.isFinite(desired) ? desired : 0;
}
