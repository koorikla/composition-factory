// Type augmentations for vanilla DOM interactions in web-proto
interface EventTarget {
  closest?(selector: string): Element | null;
  matches?(selector: string): boolean;
  classList?: DOMTokenList;
  id?: string;
  value?: string;
  tagName?: string;
  isContentEditable?: boolean;
}

interface Event {
  key?: string;
}

interface Element {
  style?: any;
  disabled?: boolean;
  value?: string;
  checked?: boolean;
  type?: string;
  selectionStart?: number;
  selectionEnd?: number;
  focus?(): void;
  blur?(): void;
  click?(): void;
  onclick?: ((this: GlobalEventHandlers, ev: MouseEvent) => any) | null;
  offsetHeight?: number;
  hidden?: any;
  files?: FileList | null;
}

interface HTMLElement {
  files?: FileList | null;
}

interface Window {
  store?: any;
  clearErrorToast?: () => void;
}
