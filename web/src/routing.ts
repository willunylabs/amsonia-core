import { useEffect, useState, useRef } from "react";

export function navigate(path: string) {
  location.hash = `/${path.replace(/^\/+/, "")}`;
}

export function useConsoleRoute() {
  const read = () =>
    location.hash.startsWith("#/") ? location.hash.slice(2) : "overview";
  const [path, setPath] = useState(read);
  useEffect(() => {
    const changed = () => setPath(read());
    window.addEventListener("hashchange", changed);
    return () => window.removeEventListener("hashchange", changed);
  }, []);
  let segments: string[];
  try {
    segments = path.split("/").map(decodeURIComponent);
  } catch {
    segments = ["not-found"];
  }
  return { path, segments, navigate };
}

export function useAlive() {
  const alive = useRef(false);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  return alive;
}
