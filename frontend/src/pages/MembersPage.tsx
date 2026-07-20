// Members & invites: list members with role badges, remove member (OWNER),
// invite by email (OWNER), pending invites with revoke (OWNER). API error
// codes (403 non-owner, 409 duplicate, 422 last-owner) surface as friendly
// inline messages.

import { useCallback, useEffect, useState, type FormEvent } from "react";

import { useAuth } from "../auth/AuthContext";
import { ApiError, LIMIT_REACHED, endpoints } from "../lib/api";
import type { Invite, Member } from "../lib/types";
import { usePageTitle } from "../lib/usePageTitle";
import {
  ErrorBanner,
  InitialAvatar,
  NoticeBanner,
  PageLoader,
  RoleBadge,
  Spinner,
} from "../components/ui";

function friendlyError(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    // LIMIT_REACHED is also a 403 — its server message wins over the
    // owner-role hint (callers show it via NoticeBanner where applicable).
    if (err.code === LIMIT_REACHED) return err.message;
    if (err.status === 403) return "Only an organization owner can do this.";
    if (err.message) return capitalize(err.message);
  }

  return fallback;
}

function capitalize(s: string): string {
  return s.length > 0 ? s[0].toUpperCase() + s.slice(1) : s;
}

function formatDate(iso: string): string {
  const date = new Date(iso);

  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

export default function MembersPage() {
  usePageTitle("Members");

  const { user, role } = useAuth();
  const isOwner = role === "OWNER";

  const [members, setMembers] = useState<Member[]>([]);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");

  const load = useCallback(async () => {
    setLoadError("");

    try {
      const [memberList, inviteList] = await Promise.all([
        endpoints.members(),
        endpoints.invites(),
      ]);

      setMembers(memberList);
      setInvites(inviteList);
    } catch (err: unknown) {
      setLoadError(friendlyError(err, "Could not load members. Please try again."));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // --- remove member (two-click inline confirm) ---
  const [confirmRemoveId, setConfirmRemoveId] = useState(0);
  const [removeBusyId, setRemoveBusyId] = useState(0);
  const [removeError, setRemoveError] = useState("");

  const removeMember = async (userId: number) => {
    setRemoveBusyId(userId);
    setRemoveError("");

    try {
      await endpoints.removeMember(userId);
      setMembers((prev) => prev.filter((m) => m.user_id !== userId));
    } catch (err: unknown) {
      setRemoveError(friendlyError(err, "Could not remove this member."));
    } finally {
      setRemoveBusyId(0);
      setConfirmRemoveId(0);
    }
  };

  // --- invites ---
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteBusy, setInviteBusy] = useState(false);
  const [inviteError, setInviteError] = useState("");
  const [inviteNotice, setInviteNotice] = useState("");
  // LIMIT_REACHED denial: the server's message, shown verbatim as a notice.
  const [limitNotice, setLimitNotice] = useState("");
  const [revokeBusyId, setRevokeBusyId] = useState(0);
  const [revokeError, setRevokeError] = useState("");

  const submitInvite = async (event: FormEvent) => {
    event.preventDefault();

    const email = inviteEmail.trim().toLowerCase();
    if (!email || inviteBusy) return;

    setInviteBusy(true);
    setInviteError("");
    setInviteNotice("");
    setLimitNotice("");

    try {
      const invite = await endpoints.createInvite(email);

      setInvites((prev) => [...prev, invite]);
      setInviteEmail("");
      setInviteNotice(`Invited ${email}. They will join when they sign in.`);
    } catch (err: unknown) {
      if (err instanceof ApiError && err.code === LIMIT_REACHED) {
        setLimitNotice(err.message);
      } else {
        setInviteError(friendlyError(err, "Could not send the invite."));
      }
    } finally {
      setInviteBusy(false);
    }
  };

  const revokeInvite = async (inviteId: number) => {
    setRevokeBusyId(inviteId);
    setRevokeError("");

    try {
      await endpoints.revokeInvite(inviteId);
      setInvites((prev) => prev.filter((i) => i.id !== inviteId));
    } catch (err: unknown) {
      setRevokeError(friendlyError(err, "Could not revoke the invite."));
    } finally {
      setRevokeBusyId(0);
    }
  };

  if (loading) return <PageLoader />;

  if (loadError) {
    return (
      <ErrorBanner
        message={loadError}
        onRetry={() => {
          setLoading(true);
          void load();
        }}
      />
    );
  }

  return (
    <div className="space-y-8">
      {/* Members */}
      <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-slate-100 px-4 py-4 sm:px-6">
          <div>
            <h2 className="text-base font-semibold text-slate-900">Members</h2>
            <p className="mt-0.5 text-sm text-slate-500">
              {members.length} {members.length === 1 ? "person" : "people"} in this organization
            </p>
          </div>
        </div>

        {removeError && (
          <div className="px-4 pt-4 sm:px-6">
            <ErrorBanner message={removeError} />
          </div>
        )}

        <ul className="divide-y divide-slate-100" data-testid="members-list">
          {members.map((member) => (
            <li
              key={member.user_id}
              className="flex flex-wrap items-center gap-3 px-4 py-3.5 sm:px-6"
            >
              <InitialAvatar name={member.name} />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-slate-900">
                  {member.name}
                  {member.user_id === user?.id && (
                    <span className="ml-1.5 text-xs font-normal text-slate-400">(you)</span>
                  )}
                </p>
                <p className="truncate text-sm text-slate-500">{member.email}</p>
              </div>

              <span className="hidden text-xs text-slate-400 sm:block">
                Joined {formatDate(member.joined_at)}
              </span>

              <RoleBadge role={member.role} />

              {isOwner &&
                (confirmRemoveId === member.user_id ? (
                  <span className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() => void removeMember(member.user_id)}
                      disabled={removeBusyId !== 0}
                      className="rounded-md bg-red-600 px-2.5 py-1 text-xs font-semibold text-white hover:bg-red-500 disabled:opacity-50"
                    >
                      {removeBusyId === member.user_id ? "Removing…" : "Confirm"}
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmRemoveId(0)}
                      disabled={removeBusyId !== 0}
                      className="rounded-md px-2 py-1 text-xs font-medium text-slate-500 hover:text-slate-700"
                    >
                      Cancel
                    </button>
                  </span>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      setConfirmRemoveId(member.user_id);
                      setRemoveError("");
                    }}
                    className="rounded-md px-2.5 py-1 text-xs font-medium text-red-600 transition hover:bg-red-50"
                  >
                    Remove
                  </button>
                ))}
            </li>
          ))}
        </ul>
      </section>

      {/* Invites */}
      <section className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="border-b border-slate-100 px-4 py-4 sm:px-6">
          <h2 className="text-base font-semibold text-slate-900">Invites</h2>
          <p className="mt-0.5 text-sm text-slate-500">
            Invited people join as members the first time they sign in.
          </p>
        </div>

        <div className="space-y-4 px-4 py-4 sm:px-6">
          {isOwner ? (
            <form onSubmit={submitInvite} className="flex flex-col gap-2 sm:flex-row">
              <label htmlFor="invite-email" className="sr-only">
                Email address
              </label>
              <input
                id="invite-email"
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                placeholder="teammate@example.com"
                required
                className="w-full flex-1 rounded-lg border border-slate-300 px-3 py-2 text-sm text-slate-900 shadow-sm placeholder:text-slate-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-200"
              />
              <button
                type="submit"
                disabled={inviteBusy || inviteEmail.trim() === ""}
                className="flex items-center justify-center gap-2 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {inviteBusy && <Spinner className="h-4 w-4 text-white" />}
                {inviteBusy ? "Inviting…" : "Send invite"}
              </button>
            </form>
          ) : (
            <p className="rounded-lg bg-slate-50 px-4 py-3 text-sm text-slate-500">
              Only organization owners can invite or remove people.
            </p>
          )}

          {inviteError && <ErrorBanner message={inviteError} />}
          {limitNotice && <NoticeBanner message={limitNotice} />}
          {inviteNotice && (
            <p
              className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700"
              role="status"
            >
              {inviteNotice}
            </p>
          )}
          {revokeError && <ErrorBanner message={revokeError} />}

          {invites.length === 0 ? (
            <p className="text-sm text-slate-400">No pending invites.</p>
          ) : (
            <ul className="divide-y divide-slate-100" data-testid="invites-list">
              {invites.map((invite) => (
                <li key={invite.id} className="flex flex-wrap items-center gap-3 py-3">
                  <InitialAvatar name={invite.email} className="h-8 w-8 text-xs" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-slate-900">{invite.email}</p>
                    <p className="text-xs text-slate-400">
                      Invited {formatDate(invite.created_at)}
                    </p>
                  </div>
                  <span className="inline-flex items-center rounded-full bg-amber-50 px-2.5 py-0.5 text-xs font-medium text-amber-700 ring-1 ring-inset ring-amber-200">
                    pending
                  </span>
                  {isOwner && (
                    <button
                      type="button"
                      onClick={() => void revokeInvite(invite.id)}
                      disabled={revokeBusyId !== 0}
                      className="rounded-md px-2.5 py-1 text-xs font-medium text-red-600 transition hover:bg-red-50 disabled:opacity-50"
                    >
                      {revokeBusyId === invite.id ? "Revoking…" : "Revoke"}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>
    </div>
  );
}
