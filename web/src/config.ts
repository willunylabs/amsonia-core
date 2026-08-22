function requiredURL(name: string, rawValue: string | undefined, developmentFallback: string): string {
  const value = String(rawValue || '').trim();
  const candidate = value || (import.meta.env.DEV ? developmentFallback : '');
  try {
    const parsed = new URL(candidate);
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') throw new Error('unsupported protocol');
    return parsed.toString();
  } catch {
    throw new Error(`${name} must be configured with an absolute HTTP(S) URL`);
  }
}

export const SOURCE_URL = requiredURL(
  'VITE_GITHUB_URL',
  import.meta.env.VITE_GITHUB_URL,
  'https://github.com/willunylabs/amsonia-core'
);

export const COMMERCIAL_URL = requiredURL(
  'VITE_COMMERCIAL_URL',
  import.meta.env.VITE_COMMERCIAL_URL,
  'http://localhost:3001/amsonia'
);
