import {useId} from 'react';

// Inline copy of docs/brand/weft-mark.svg. Colours come from the --brand-warp
// and --brand-weft tokens so the mark follows the active theme.
export function BrandMark({size = 24}: {size?: number}) {
  const id = useId().replace(/:/g, '');
  const warp = `weft-w-${id}`;
  const thread = `weft-t-${id}`;
  return (
    <svg className="brand-mark" viewBox="0 0 64 64" width={size} height={size} aria-hidden="true" focusable="false">
      <defs>
        <mask id={warp} maskUnits="userSpaceOnUse" x="0" y="0" width="64" height="64">
          <rect width="64" height="64" fill="#fff" />
          <rect x="9" y="24.6" width="12.5" height="10.8" fill="#000" />
          <rect x="42.5" y="24.6" width="12.5" height="10.8" fill="#000" />
        </mask>
        <mask id={thread} maskUnits="userSpaceOnUse" x="0" y="0" width="64" height="64">
          <rect width="64" height="64" fill="#fff" />
          <polyline points="21,48 32,24 43,48" fill="none" stroke="#000" strokeWidth="11.5" strokeLinejoin="round" />
        </mask>
      </defs>
      <g fill="none" strokeLinecap="round" strokeLinejoin="round" strokeWidth="6.5">
        <polyline className="brand-warp" points="10,14 21,48 32,24 43,48 54,14" mask={`url(#${warp})`} />
        <line className="brand-weft" x1="4" y1="30" x2="60" y2="30" mask={`url(#${thread})`} />
      </g>
    </svg>
  );
}
