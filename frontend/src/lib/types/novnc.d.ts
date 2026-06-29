declare module '@novnc/novnc' {
	interface RFBOptions {
		credentials?: Record<string, string>;
		repeaterID?: string;
		shared?: boolean;
		wsProtocols?: string[];
	}

	export default class RFB extends EventTarget {
		constructor(target: HTMLElement, urlOrChannel: string | WebSocket | RTCDataChannel, options?: RFBOptions);

		scaleViewport: boolean;
		resizeSession: boolean;
		viewOnly: boolean;
		focusOnClick: boolean;
		qualityLevel: number;
		compressionLevel: number;
		background: string;

		disconnect(): void;
		sendCredentials(credentials: Record<string, string>): void;
		clipboardPasteFrom(text: string): void;
	}
}
