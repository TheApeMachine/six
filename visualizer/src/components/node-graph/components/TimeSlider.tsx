/**
 * TimeSlider - Temporal range selector for graph visualization
 *
 * Provides a dual-handle range slider for filtering graph events by time.
 * Supports playback animation to visualize temporal patterns.
 */

import {
    forwardRef,
    type MouseEvent as ReactMouseEvent,
    useCallback,
    useEffect,
    useImperativeHandle,
    useRef,
    useState
} from "react";

export interface TimeSliderHandle {
    togglePlay: () => void;
    toggleRepeat: () => void;
    expandRange: () => void;
    isPlaying: boolean;
    isRepeating: boolean;
    isExpanded: boolean;
}

export interface TimeSliderProps {
    /** Minimum timestamp in seconds */
    min: number;
    /** Maximum timestamp in seconds */
    max: number;
    /** Current range start */
    from: number;
    /** Current range end */
    to: number;
    /** Callback when range changes */
    onChange: (from: number, to: number) => void;
    /** Format function for displaying timestamps */
    formatTime?: (timestamp: number) => string;
    /** Step size for playback */
    stepSize?: number;
    /** CSS class name */
    className?: string;
    /** Show built-in controls (default: true) */
    showControls?: boolean;
    /** Use legacy ionRangeSlider skinFlat appearance */
    legacyAppearance?: boolean;
    /** Callback when playback state changes */
    onPlaybackChange?: (
        isPlaying: boolean,
        isRepeating: boolean,
        isExpanded: boolean
    ) => void;
}

/**
 * Default time formatter
 */
function defaultFormatTime(timestamp: number): string {
    // Legacy format: "MMM Do, hh:mm A" - approximation
    const date = new Date(timestamp * 1000);
    return date.toLocaleString("en-US", {
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        month: "short"
    });
}

/**
 * TimeSlider component
 *
 * A dual-handle range slider for temporal data filtering with playback controls.
 */
export const TimeSlider = forwardRef<TimeSliderHandle, TimeSliderProps>(
    (
        {
            min,
            max,
            from,
            to,
            onChange,
            formatTime = defaultFormatTime,
            stepSize = 300,
            className,
            showControls = true,
            legacyAppearance = false,
            onPlaybackChange
        },
        ref
    ) => {
        const [isPlaying, setIsPlaying] = useState(false);
        const [isRepeating, setIsRepeating] = useState(true);
        const [isExpanded, setIsExpanded] = useState(false);
        const [previousRange, setPreviousRange] = useState<{
            from: number;
            to: number;
        } | null>(null);
        const [currentFrom, setCurrentFrom] = useState(from);
        const [currentTo, setCurrentTo] = useState(to);
        const [isDragging, setIsDragging] = useState<
            "from" | "to" | "range" | null
        >(null);

        const sliderRef = useRef<HTMLDivElement>(null);
        const animationRef = useRef<number>(0);

        const baseStepRef = useRef<number>(0);
        const speedMultiplierRef = useRef<number>(1);

        const currentFromRef = useRef<number>(from);
        const currentToRef = useRef<number>(to);

        const isPlayingRef = useRef<boolean>(false);
        const isRepeatingRef = useRef<boolean>(true);

        const rangeDragOffsetRef = useRef<number>(0);

        // Notify parent of state changes
        useEffect(() => {
            onPlaybackChange?.(isPlaying, isRepeating, isExpanded);
        }, [isPlaying, isRepeating, isExpanded, onPlaybackChange]);

        // Sync with props
        useEffect(() => {
            if (isDragging) return;
            setCurrentFrom(from);
            setCurrentTo(to);
            currentFromRef.current = from;
            currentToRef.current = to;
        }, [from, to, isDragging]);

        useEffect(() => {
            const duration = max - min;
            if (!Number.isFinite(duration) || duration <= 0) {
                baseStepRef.current = 0;
                return;
            }
            baseStepRef.current = duration / stepSize;
        }, [min, max, stepSize]);

        useEffect(() => {
            isPlayingRef.current = isPlaying;
        }, [isPlaying]);

        useEffect(() => {
            isRepeatingRef.current = isRepeating;
        }, [isRepeating]);

        /**
         * Toggle playback
         */
        const togglePlay = useCallback(() => {
            setIsPlaying((prev) => {
                const next = !prev;

                if (next) {
                    const currentFromValue = currentFromRef.current;
                    const currentToValue = currentToRef.current;
                    const duration = currentToValue - currentFromValue;

                    if (currentToValue >= max) {
                        const clampedDuration = Math.min(
                            Math.max(duration, 0),
                            max - min
                        );
                        const nextFrom = min;
                        const nextTo = min + clampedDuration;
                        currentFromRef.current = nextFrom;
                        currentToRef.current = nextTo;
                        setCurrentFrom(nextFrom);
                        setCurrentTo(nextTo);
                        onChange(nextFrom, nextTo);
                    }
                }

                isPlayingRef.current = next;
                return next;
            });
        }, [min, max, onChange]);

        /**
         * Toggle repeat mode
         */
        const toggleRepeat = useCallback(() => {
            setIsRepeating((prev) => {
                const next = !prev;
                isRepeatingRef.current = next;
                return next;
            });
        }, []);

        /**
         * Expand to full range or restore
         */
        const expandRange = useCallback(() => {
            if (isExpanded && previousRange) {
                setCurrentFrom(previousRange.from);
                setCurrentTo(previousRange.to);
                currentFromRef.current = previousRange.from;
                currentToRef.current = previousRange.to;
                onChange(previousRange.from, previousRange.to);
                setPreviousRange(null);
                setIsExpanded(false);
                return;
            }

            setPreviousRange({
                from: currentFromRef.current,
                to: currentToRef.current
            });
            setCurrentFrom(min);
            setCurrentTo(max);
            currentFromRef.current = min;
            currentToRef.current = max;
            onChange(min, max);
            isPlayingRef.current = false;
            setIsPlaying(false);
            setIsExpanded(true);
        }, [min, max, onChange, isExpanded, previousRange]);

        useImperativeHandle(ref, () => ({
            expandRange,
            isExpanded,
            isPlaying,
            isRepeating,
            togglePlay,
            toggleRepeat
        }));

        // Animation loop for playback
        useEffect(() => {
            if (!isPlaying) {
                cancelAnimationFrame(animationRef.current);
                return;
            }

            const animate = (_currentTime: number) => {
                const duration = currentToRef.current - currentFromRef.current;
                const clampedDuration = Math.min(
                    Math.max(duration, 0),
                    max - min
                );

                const stepPerFrame =
                    baseStepRef.current * speedMultiplierRef.current;

                let nextFrom = currentFromRef.current + stepPerFrame;

                let nextTo = nextFrom + clampedDuration;

                if (nextTo >= max) {
                    if (isRepeatingRef.current) {
                        nextFrom = min;
                        nextTo = min + clampedDuration;
                    } else {
                        nextFrom = max - clampedDuration;
                        nextTo = max;
                        isPlayingRef.current = false;
                        setIsPlaying(false);
                    }
                }

                currentFromRef.current = nextFrom;
                currentToRef.current = nextTo;
                setCurrentFrom(nextFrom);
                setCurrentTo(nextTo);
                onChange(nextFrom, nextTo);

                if (isPlayingRef.current) {
                    animationRef.current = requestAnimationFrame(animate);
                }
            };

            isPlayingRef.current = true;
            animationRef.current = requestAnimationFrame(animate);

            return () => cancelAnimationFrame(animationRef.current);
        }, [isPlaying, min, max, onChange]);

        /**
         * Convert pixel position to timestamp
         */
        const positionToValue = useCallback(
            (clientX: number): number => {
                if (!sliderRef.current) return min;
                const rect = sliderRef.current.getBoundingClientRect();
                const percent = Math.max(
                    0,
                    Math.min(1, (clientX - rect.left) / rect.width)
                );
                return min + percent * (max - min);
            },
            [min, max]
        );

        /**
         * Convert timestamp to percentage
         */
        const valueToPercent = useCallback(
            (value: number): number => {
                return ((value - min) / (max - min)) * 100;
            },
            [min, max]
        );

        /**
         * Handle mouse down on slider
         */
        const handleMouseDown = useCallback(
            (e: ReactMouseEvent, target: "from" | "to" | "range") => {
                e.preventDefault();

                if (isPlayingRef.current) {
                    isPlayingRef.current = false;
                    setIsPlaying(false);
                }

                if (target === "range") {
                    const pointerValue = positionToValue(e.clientX);
                    rangeDragOffsetRef.current =
                        pointerValue - currentFromRef.current;
                }

                setIsDragging(target);
            },
            [positionToValue]
        );

        /**
         * Handle mouse move
         */
        useEffect(() => {
            if (!isDragging) return;

            const minGap = 0;

            const handleMouseMove = (e: MouseEvent) => {
                const value = positionToValue(e.clientX);

                if (isDragging === "from") {
                    const maxFrom = currentToRef.current - minGap;
                    const nextFrom = Math.min(value, maxFrom);
                    const clampedFrom = Math.max(min, nextFrom);
                    currentFromRef.current = clampedFrom;
                    setCurrentFrom(clampedFrom);
                    onChange(clampedFrom, currentToRef.current);
                    return;
                }

                if (isDragging === "to") {
                    const minTo = currentFromRef.current + minGap;
                    const nextTo = Math.max(value, minTo);
                    const clampedTo = Math.min(max, nextTo);
                    currentToRef.current = clampedTo;
                    setCurrentTo(clampedTo);
                    onChange(currentFromRef.current, clampedTo);
                    return;
                }

                const duration = currentToRef.current - currentFromRef.current;
                const clampedDuration = Math.min(
                    Math.max(duration, 0),
                    max - min
                );

                let nextFrom = value - rangeDragOffsetRef.current;
                let nextTo = nextFrom + clampedDuration;

                if (nextFrom < min) {
                    nextFrom = min;
                    nextTo = min + clampedDuration;
                }

                if (nextTo > max) {
                    nextTo = max;
                    nextFrom = max - clampedDuration;
                }

                currentFromRef.current = nextFrom;
                currentToRef.current = nextTo;
                setCurrentFrom(nextFrom);
                setCurrentTo(nextTo);
                onChange(nextFrom, nextTo);
            };

            const handleMouseUp = () => {
                setIsDragging(null);
            };

            window.addEventListener("mousemove", handleMouseMove);
            window.addEventListener("mouseup", handleMouseUp);

            return () => {
                window.removeEventListener("mousemove", handleMouseMove);
                window.removeEventListener("mouseup", handleMouseUp);
            };
        }, [isDragging, min, max, positionToValue, onChange]);

        const increaseSpeed = useCallback(() => {
            speedMultiplierRef.current *= 1.25;
        }, []);

        const decreaseSpeed = useCallback(() => {
            speedMultiplierRef.current *= 0.75;
        }, []);

        const clampPercent = (value: number): number => {
            if (!Number.isFinite(value)) return 0;
            if (value <= 0) return 0;
            if (value >= 100) return 100;
            const rounded = Math.round(value * 1000) / 1000;
            if (rounded > 99.999) return 100;
            if (rounded < 0.001) return 0;
            return rounded;
        };

        const fromPercent = clampPercent(valueToPercent(currentFrom));
        const toPercent = clampPercent(valueToPercent(currentTo));
        const leftPercent = Math.min(fromPercent, toPercent);
        const barWidthPercent = Math.min(
            100 - leftPercent,
            Math.max(0, toPercent - leftPercent)
        );

        if (legacyAppearance) {
            return (
                <div
                    className={`irs irs-with-grid ${className ?? ""}`}
                    ref={sliderRef}
                    style={{ width: "100%" }}
                >
                    <div className="irs-line">
                        <div className="irs-line-left"></div>
                        <div className="irs-line-mid"></div>
                        <div className="irs-line-right"></div>
                        <div
                            className="irs-bar"
                            style={{
                                left: `${leftPercent}%`,
                                width: `${barWidthPercent}%`
                            }}
                            onMouseDown={(e) => handleMouseDown(e, "range")}
                        ></div>
                        {/* Legacy skin uses a small rounded "edge" piece; we must position it with the bar. */}
                        <div
                            className="irs-bar-edge"
                            style={{
                                left: `${leftPercent}%`,
                                display: barWidthPercent > 0 ? "block" : "none"
                            }}
                        ></div>
                    </div>
                    <div className="irs-min" style={{ visibility: "visible" }}>
                        {formatTime(min)}
                    </div>
                    <div className="irs-max" style={{ visibility: "visible" }}>
                        {formatTime(max)}
                    </div>
                    <div
                        className="irs-from"
                        style={{
                            left: `${fromPercent}%`,
                            marginLeft: "-25px",
                            visibility: "visible"
                        }} // Approximate centering fix
                    >
                        {formatTime(currentFrom)}
                    </div>
                    <div
                        className="irs-to"
                        style={{
                            left: `${toPercent}%`,
                            marginLeft: "-25px",
                            visibility: "visible"
                        }}
                    >
                        {formatTime(currentTo)}
                    </div>
                    <div
                        className="irs-slider from"
                        style={{ left: `${fromPercent-1}%` }}
                        onMouseDown={(e) => handleMouseDown(e, "from")}
                    ></div>
                    <div
                        className="irs-slider to"
                        style={{ left: `${toPercent-1}%` }}
                        onMouseDown={(e) => handleMouseDown(e, "to")}
                    ></div>
                </div>
            );
        }

        return (
            <div className={`time-slider ${className ?? ""}`}>
                <div className="time-slider-track" ref={sliderRef}>
                    {/* Background track */}
                    <div className="track-bg" />

                    {/* Active range */}
                    <div
                        className="track-range"
                        style={{
                            left: `${fromPercent}%`,
                            width: `${toPercent - fromPercent}%`
                        }}
                        onMouseDown={(e) => handleMouseDown(e, "range")}
                    />

                    {/* From handle */}
                    <div
                        className={`track-handle handle-from ${isDragging === "from" ? "active" : ""}`}
                        style={{ left: `${fromPercent}%` }}
                        onMouseDown={(e) => handleMouseDown(e, "from")}
                    >
                        <div className="handle-tooltip">
                            {formatTime(currentFrom)}
                        </div>
                    </div>

                    {/* To handle */}
                    <div
                        className={`track-handle handle-to ${isDragging === "to" ? "active" : ""}`}
                        style={{ left: `${toPercent}%` }}
                        onMouseDown={(e) => handleMouseDown(e, "to")}
                    >
                        <div className="handle-tooltip">
                            {formatTime(currentTo)}
                        </div>
                    </div>
                </div>

                {/* Time labels */}
                <div className="time-slider-labels">
                    <span className="label-min">{formatTime(min)}</span>
                    <span className="label-max">{formatTime(max)}</span>
                </div>

                {/* Controls */}
                {showControls && (
                    <div className="time-slider-controls">
                        <button
                            className={`slider-btn ${isPlaying ? "playing" : ""}`}
                            onClick={togglePlay}
                            title={isPlaying ? "Pause" : "Play"}
                        >
                            {isPlaying ? (
                                <svg viewBox="0 0 16 16" fill="currentColor">
                                    <rect
                                        x="3"
                                        y="2"
                                        width="4"
                                        height="12"
                                        rx="1"
                                    />
                                    <rect
                                        x="9"
                                        y="2"
                                        width="4"
                                        height="12"
                                        rx="1"
                                    />
                                </svg>
                            ) : (
                                <svg viewBox="0 0 16 16" fill="currentColor">
                                    <path d="M4 2.5v11a.5.5 0 0 0 .75.43l9-5.5a.5.5 0 0 0 0-.86l-9-5.5A.5.5 0 0 0 4 2.5z" />
                                </svg>
                            )}
                        </button>
                        <button
                            className={`slider-btn ${isRepeating ? "active" : ""}`}
                            onClick={toggleRepeat}
                            title="Repeat"
                        >
                            <svg viewBox="0 0 16 16" fill="currentColor">
                                <path d="M11 5.466V4H5a4 4 0 0 0-4 4 .5.5 0 0 1-1 0 5 5 0 0 1 5-5h6V1.534a.25.25 0 0 1 .41-.192l2.36 1.966c.12.1.12.284 0 .384l-2.36 1.966a.25.25 0 0 1-.41-.192z" />
                                <path d="M5 10.534V12h6a4 4 0 0 0 4-4 .5.5 0 0 1 1 0 5 5 0 0 1-5 5H5v1.466a.25.25 0 0 1-.41.192l-2.36-1.966a.25.25 0 0 1 0-.384l2.36-1.966a.25.25 0 0 1 .41.192z" />
                            </svg>
                        </button>
                        <button
                            className="slider-btn"
                            onClick={expandRange}
                            title="Show All"
                        >
                            <svg viewBox="0 0 16 16" fill="currentColor">
                                <path d="M8 4a.5.5 0 0 1 .5.5v3h3a.5.5 0 0 1 0 1h-3v3a.5.5 0 0 1-1 0v-3h-3a.5.5 0 0 1 0-1h3v-3A.5.5 0 0 1 8 4z" />
                            </svg>
                        </button>
                        <button
                            className="slider-btn"
                            onClick={decreaseSpeed}
                            title="Slower"
                        >
                            <svg viewBox="0 0 16 16" fill="currentColor">
                                <path d="M8.404 7.434L10.8 5.035a.5.5 0 0 1 .848.354v5.222a.5.5 0 0 1-.848.354L8.404 8.566a.5.5 0 0 1 0-.708v-.424z" />
                                <path d="M4.404 7.434L6.8 5.035a.5.5 0 0 1 .848.354v5.222a.5.5 0 0 1-.848.354L4.404 8.566a.5.5 0 0 1 0-.708v-.424z" />
                            </svg>
                        </button>
                        <button
                            className="slider-btn"
                            onClick={increaseSpeed}
                            title="Faster"
                        >
                            <svg viewBox="0 0 16 16" fill="currentColor">
                                <path d="M7.596 7.434L5.2 5.035a.5.5 0 0 0-.848.354v5.222a.5.5 0 0 0 .848.354l2.396-2.399a.5.5 0 0 0 0-.708v-.424z" />
                                <path d="M11.596 7.434L9.2 5.035a.5.5 0 0 0-.848.354v5.222a.5.5 0 0 0 .848.354l2.396-2.399a.5.5 0 0 0 0-.708v-.424z" />
                            </svg>
                        </button>
                    </div>
                )}
            </div>
        );
    }
);

export default TimeSlider;
