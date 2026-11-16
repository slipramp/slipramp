export const oidcLoginLeaseStorageKey = "slipramp_oidc_login_lease";

const oidcLoginLockName = "slipramp-oidc-login";
const oidcLoginLeaseLifetimeMs = 10 * 60 * 1000;

type OIDCLoginClaim = "claimed" | "pending" | "unsupported";

export async function claimOIDCLogin(
  storage: Pick<Storage, "getItem" | "setItem">,
  locks: LockManager | undefined,
  options: { force?: boolean; now?: number } = {},
): Promise<OIDCLoginClaim> {
  const force = options.force ?? false;
  const now = options.now ?? Date.now();

  if (!locks) return force ? "claimed" : "unsupported";

  let claim: OIDCLoginClaim = "pending";
  try {
    await locks.request(oidcLoginLockName, async () => {
      const leaseExpiresAt = Number(storage.getItem(oidcLoginLeaseStorageKey));
      if (!force && Number.isFinite(leaseExpiresAt) && leaseExpiresAt > now) return;

      storage.setItem(oidcLoginLeaseStorageKey, String(now + oidcLoginLeaseLifetimeMs));
      claim = "claimed";
    });
  } catch {
    return force ? "claimed" : "unsupported";
  }

  return claim;
}

export function clearOIDCLoginClaim(storage: Pick<Storage, "removeItem">) {
  storage.removeItem(oidcLoginLeaseStorageKey);
}
