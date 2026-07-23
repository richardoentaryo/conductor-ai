// Small shared formatters so cost/latency/time render consistently everywhere.

export function usd(n: number): string {
  // Costs are tiny fractions of a dollar; show enough precision to be useful
  // without pretending to more than the pricing config provides.
  if (n === 0) return "$0";
  if (n < 0.01) return `$${n.toFixed(6)}`;
  return `$${n.toFixed(4)}`;
}

export function ms(n: number): string {
  if (n < 1000) return `${n} ms`;
  return `${(n / 1000).toFixed(2)} s`;
}

export function timeAgo(unix: number): string {
  const d = new Date(unix * 1000);
  return d.toLocaleString();
}
