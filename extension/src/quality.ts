import type { VideoChoice } from './protocol.js';

// Same names as the desktop app (and the engine's namedSize): people know
// resolutions from TVs and phones; codecs and containers stay internal, since
// every video is saved as MP4. A 1920×800 crop is 1080p, like a 1080×1920 Short.
export function qualityName(choice: VideoChoice): string {
  const sides = [choice.width, choice.height].filter((n): n is number => n !== undefined && n > 0);
  if (!sides.length) return 'Qualidade padrão';
  const size = sides.length === 2 ? Math.max(Math.min(...sides), Math.max(...sides) * 9 / 16) : sides[0];
  if (size >= 2160) return '4K';
  if (size >= 1440) return '1440p';
  if (size >= 1080) return '1080p Full HD';
  if (size >= 720) return '720p HD';
  return `${Math.round(size)}p`;
}
