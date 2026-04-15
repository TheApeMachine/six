/*
wheelDeltaToPixels normalizes WheelEvent deltas into approximate CSS pixels so a
physical gesture scales the same whether the browser reports pixel, line, or
page steps. Without this, line-mode wheels and pixel-mode trackpads disagree by
an order of magnitude and zoom feels uncontrollable.
*/
export function wheelDeltaToPixels(
	event: WheelEvent,
	viewWidth: number,
	viewHeight: number,
): { deltaX: number; deltaY: number } {
	const lineHeightPx = 16;
	let deltaX = event.deltaX;
	let deltaY = event.deltaY;

	if (event.deltaMode === WheelEvent.DOM_DELTA_LINE) {
		deltaX *= lineHeightPx;
		deltaY *= lineHeightPx;
	}

	if (event.deltaMode === WheelEvent.DOM_DELTA_PAGE) {
		deltaX *= viewWidth;
		deltaY *= viewHeight;
	}

	return { deltaX, deltaY };
}

/*
WHEEL_ZOOM_SENSITIVITY maps vertical pixel delta to exponential zoom: the
product of factors exp(-dy * k) over a gesture depends on total vertical travel,
so many small trackpad events compound like one proportional zoom.
*/
export const WHEEL_ZOOM_SENSITIVITY = 0.002;
