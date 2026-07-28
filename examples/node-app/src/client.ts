// Vite exposes build-time configuration on import.meta.env.
export const apiUrl = import.meta.env.VITE_API_URL;

export function describe(): string {
  return `talking to ${import.meta.env.VITE_API_URL}`;
}
