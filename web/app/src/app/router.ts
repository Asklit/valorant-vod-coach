import { useCallback, useSyncExternalStore } from "react";

export type PageID = "dashboard" | "library" | "review" | "reports" | "admin";

const pagePaths: Record<PageID, string> = {
  dashboard: "/dashboard",
  library: "/library",
  review: "/review",
  reports: "/reports",
  admin: "/admin"
};

const navigationEvent = "vodcoach:navigate";

export function routePath(page: PageID) {
  return pagePaths[page];
}

export function useAppRoute() {
  const page = useSyncExternalStore(subscribe, currentPage, () => "dashboard" as PageID);
  const navigate = useCallback((nextPage: PageID, options: { replace?: boolean; params?: Record<string, string | undefined> } = {}) => {
    const query = new URLSearchParams();
    Object.entries(options.params ?? {}).forEach(([key, value]) => {
      if (value) query.set(key, value);
    });
    const path = `${routePath(nextPage)}${query.size ? `?${query.toString()}` : ""}`;
    if (`${window.location.pathname}${window.location.search}` === path) {
      return;
    }
    if (options.replace) {
      window.history.replaceState(null, "", path);
    } else {
      window.history.pushState(null, "", path);
    }
    window.dispatchEvent(new Event(navigationEvent));
    window.scrollTo({ top: 0, behavior: "smooth" });
  }, []);
  return { page, navigate };
}

function subscribe(listener: () => void) {
  window.addEventListener("popstate", listener);
  window.addEventListener(navigationEvent, listener);
  return () => {
    window.removeEventListener("popstate", listener);
    window.removeEventListener(navigationEvent, listener);
  };
}

function currentPage(): PageID {
  const path = window.location.pathname.replace(/\/+$/, "") || "/";
  const entry = (Object.entries(pagePaths) as Array<[PageID, string]>).find(([, candidate]) => candidate === path);
  return entry?.[0] ?? "dashboard";
}
