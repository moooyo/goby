import React, { Children, cloneElement, createContext, createElement, isValidElement, useContext, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { CSSProperties, ElementType, ReactNode, Ref } from 'react';
import { createPortal } from 'react-dom';
import { resolveResponsive, spacing, sxTheme, themeColor, useSx } from './sx';
import { translateNode, translateText, useTranslation } from './i18n';
import './ui.css';

type AnyEvent = any;
export interface UIProps {
  children?: ReactNode;
  sx?: any;
  style?: CSSProperties;
  className?: string;
  component?: ElementType;
  ref?: Ref<any>;
  onClick?: (event: AnyEvent) => void;
  onChange?: (event: AnyEvent, value?: any) => void;
  onSubmit?: (event: AnyEvent) => void;
  onClose?: (event?: AnyEvent, reason?: string) => void;
  onKeyDown?: (event: AnyEvent) => void;
  onFocus?: (event: AnyEvent) => void;
  onBlur?: (event: AnyEvent) => void;
  onPaste?: (event: React.ClipboardEvent<HTMLInputElement>) => void;
  [key: string]: any;
}
export interface TextFieldProps extends UIProps {
  label?: ReactNode;
  helperText?: ReactNode;
  inputRef?: Ref<any>;
  onChange?: (event: React.ChangeEvent<HTMLInputElement>) => void;
}
export interface ChipProps extends UIProps { color?: 'default' | 'primary' | 'secondary' | 'success' | 'warning' | 'error' | 'info'; }

const ignored = new Set(['sx', 'component', 'variant', 'color', 'size', 'align', 'alignItems', 'justifyContent', 'direction', 'spacing', 'divider', 'useFlexGap', 'elevation', 'square', 'fullWidth', 'maxWidth', 'fullScreen', 'disableElevation', 'disableRipple', 'disableFocusRipple', 'edge', 'startIcon', 'endIcon', 'slotProps', 'slots', 'InputProps', 'inputProps', 'InputLabelProps', 'FormHelperTextProps', 'margin', 'noWrap', 'gutterBottom', 'paragraph', 'fontSize', 'label', 'helperText', 'error', 'select', 'multiline', 'minRows', 'maxRows', 'inputRef', 'loading', 'valueLabelDisplay', 'exclusive', 'selectionFollowsFocus', 'scrollButtons', 'allowScrollButtonsMobile', 'icon', 'iconPosition', 'disableGutters', 'disablePadding', 'dense', 'inset', 'secondaryAction', 'primary', 'secondary', 'secondaryTypographyProps', 'primaryTypographyProps', 'keepMounted', 'TransitionProps', 'TransitionComponent', 'PaperProps', 'BackdropProps', 'disableEscapeKeyDown', 'disableEnforceFocus', 'disableRestoreFocus', 'hideBackdrop', 'container', 'anchorEl', 'anchorOrigin', 'transformOrigin', 'anchorReference', 'anchorPosition', 'MenuProps', 'displayEmpty', 'renderValue', 'labelId', 'native', 'onDelete', 'deleteIcon', 'autoHideDuration', 'resumeHideDuration', 'action', 'severity', 'iconMapping', 'in', 'timeout', 'unmountOnExit', 'mountOnEnter', 'orientation', 'expanded', 'defaultExpanded', 'expandIcon', 'disableSticky', 'stickyHeader', 'rowsPerPageOptions', 'labelRowsPerPage', 'labelDisplayedRows', 'getItemAriaLabel', 'onPageChange', 'onRowsPerPageChange', 'showFirstButton', 'showLastButton', 'count', 'page', 'rowsPerPage', 'SelectProps', 'ActionsComponent', 'ToolbarProps', 'TabIndicatorProps', 'TabScrollButtonProps', 'visibleScrollbar', 'wrapped', 'focusVisibleClassName', 'touchRippleRef', 'centerRipple', 'titleAccess', 'htmlColor']);
function nativeProps(props: UIProps): Record<string, any> {
  const result: Record<string, any> = {};
  for (const [key, value] of Object.entries(props)) {
    if (!ignored.has(key) && !['control', 'labelPlacement', 'selected', 'hover', 'input', 'indeterminate', 'describeChild', 'placement', 'arrow', 'enterDelay', 'leaveDelay'].includes(key) && key !== 'children' && value !== undefined) result[key] = value;
  }
  return result;
}
function cx(...values: (string | false | undefined)[]) { return values.filter(Boolean).join(' '); }
function preservesTranslation(props: Record<string, unknown>) {
  return props.translate === 'no' || props['data-no-translate'] !== undefined && props['data-no-translate'] !== false;
}
function usePresentation(props: UIProps, className: string, defaultStyle?: CSSProperties) {
  const sx = useSx(props.sx);
  const { t, language } = useTranslation();
  const attributes = nativeProps(props);
  const preserveContent = preservesTranslation(props);
  if (!preserveContent) for (const name of ['aria-label', 'title', 'placeholder']) if (typeof attributes[name] === 'string') attributes[name] = t(attributes[name]);
  attributes.className = cx(className, props.className);
  attributes.style = { ...defaultStyle, ...sx, ...props.style };
  return { attributes, content: preserveContent ? props.children : translateNode(props.children, language), t, language, preserveContent };
}
function plainText(node: ReactNode): string {
  if (node == null || typeof node === 'boolean') return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(plainText).join('');
  return isValidElement<{ children?: ReactNode }>(node) ? plainText(node.props.children) : '';
}
function translatedPlainText(node: ReactNode, language: 'zh' | 'en', preserve = false): string {
  if (node == null || typeof node === 'boolean') return '';
  if (typeof node === 'number') return String(node);
  if (typeof node === 'string') return preserve ? node : translateText(node, language);
  if (Array.isArray(node)) return node.map((child) => translatedPlainText(child, language, preserve)).join('');
  if (!isValidElement<UIProps>(node)) return '';
  const raw = preserve || preservesTranslation(node.props) || ['code', 'pre'].includes(String(node.type));
  return translatedPlainText(node.props.children ?? node.props.primary ?? node.props.label, language, raw);
}
function nativeOptions(children: ReactNode, language: 'zh' | 'en', preserve = false): ReactNode {
  return Children.toArray(children).flatMap((child): ReactNode[] => {
    if (!isValidElement<UIProps>(child)) return [];
    const raw = preserve || preservesTranslation(child.props);
    if (child.type === React.Fragment) return Children.toArray(nativeOptions(child.props.children, language, raw));
    return [<option key={String(child.props.value)} value={child.props.value} disabled={child.props.disabled} translate={raw ? 'no' : undefined}>{translatedPlainText(child.props.children ?? child.props.primary ?? child.props.label, language, raw)}</option>];
  });
}
function labelButtonProps(props: UIProps, attributes: Record<string, any>) {
  delete attributes.disabled;
  delete attributes.type;
  attributes.role ??= 'button';
  attributes.tabIndex = props.disabled ? -1 : props.tabIndex ?? 0;
  attributes['aria-disabled'] = props.disabled ? true : props['aria-disabled'];
  attributes.onClick = (event: React.MouseEvent<HTMLLabelElement>) => {
    if (props.disabled) { event.preventDefault(); event.stopPropagation(); return; }
    props.onClick?.(event);
  };
  attributes.onKeyDown = (event: React.KeyboardEvent<HTMLLabelElement>) => {
    props.onKeyDown?.(event);
    if (event.defaultPrevented || event.target !== event.currentTarget || !['Enter', ' ', 'Spacebar'].includes(event.key)) return;
    event.preventDefault();
    if (props.disabled || event.repeat) return;
    const control = event.currentTarget.control ?? event.currentTarget.querySelector<HTMLInputElement>('input[type="file"]');
    if (control) { if (!control.matches(':disabled')) control.click(); }
    else event.currentTarget.click();
  };
}
function setRef(ref: Ref<any> | undefined, value: any) {
  if (typeof ref === 'function') ref(value);
  else if (ref) (ref as React.MutableRefObject<any>).current = value;
}
export function useMediaQuery(query: string | ((theme: typeof sxTheme) => string)) {
  const resolved = (typeof query === 'function' ? query(sxTheme) : query).replace(/^@media\s*/, '');
  const [matches, setMatches] = useState(() => typeof window !== 'undefined' && window.matchMedia(resolved).matches);
  useEffect(() => { const media = window.matchMedia(resolved); const change = () => setMatches(media.matches); change(); media.addEventListener('change', change); return () => media.removeEventListener('change', change); }, [resolved]);
  return matches;
}

export function Box(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-box');
  return createElement(props.component ?? 'div', { ...attributes, ref: props.ref }, content);
}
export function Stack(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-stack MuiStack-root', { display: 'flex', flexDirection: resolveResponsive(props.direction ?? 'column') as CSSProperties['flexDirection'], gap: spacing(resolveResponsive(props.spacing ?? 0)) as CSSProperties['gap'] });
  if (props.alignItems) attributes.style.alignItems = resolveResponsive(props.alignItems);
  if (props.justifyContent) attributes.style.justifyContent = resolveResponsive(props.justifyContent);
  const values = Children.toArray(content);
  const children = props.divider ? values.flatMap((child, i) => i ? [isValidElement(props.divider) ? cloneElement(props.divider, { key: `divider-${i}` }) : props.divider, child] : [child]) : content;
  return createElement(props.component ?? 'div', { ...attributes, ref: props.ref }, children);
}
export function Typography(props: UIProps) {
  const variant = props.variant ?? 'body1';
  const { attributes, content } = usePresentation(props, cx('goby-type MuiTypography-root', `goby-type-${variant}`, props.noWrap && 'goby-nowrap', props.gutterBottom && 'goby-type-gutter'), { color: themeColor(props.color), textAlign: props.align });
  const component = props.component ?? (props.paragraph ? 'p' : /^h[1-6]$/.test(variant) ? variant : variant === 'caption' || variant === 'overline' ? 'span' : 'p');
  return createElement(component, { ...attributes, ref: props.ref }, content);
}
export function Paper(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('goby-paper MuiPaper-root', props.variant === 'outlined' && 'goby-paper-outlined', props.square && 'goby-square'));
  return createElement(props.component ?? 'div', { ...attributes, ref: props.ref }, content);
}
export function ButtonBase(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-button-base MuiButtonBase-root');
  const component = props.component ?? (props.href ? 'a' : 'button');
  if (component === 'button') attributes.type ??= 'button';
  if (component === 'label') labelButtonProps(props, attributes);
  if (props.disabled && component === 'a') { attributes['aria-disabled'] = true; attributes.tabIndex = -1; attributes.onClick = (event: React.MouseEvent) => event.preventDefault(); }
  return createElement(component, { ...attributes, ref: props.ref }, content);
}
export function Button(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('goby-button MuiButton-root', `goby-button-${props.variant ?? 'text'}`, `goby-tone-${props.color ?? 'primary'}`, props.size === 'small' && 'goby-small', props.fullWidth && 'goby-full-width'));
  const component = props.component ?? (props.href ? 'a' : 'button');
  if (component === 'button') attributes.type ??= 'button';
  if (component === 'label') labelButtonProps(props, attributes);
  if (props.disabled && component === 'a') { attributes['aria-disabled'] = true; attributes.tabIndex = -1; attributes.onClick = (event: React.MouseEvent) => event.preventDefault(); }
  return createElement(component, { ...attributes, ref: props.ref }, <>{props.startIcon && <span className="goby-button-icon MuiButton-startIcon" aria-hidden="true">{props.startIcon}</span>}{content}{props.endIcon && <span className="goby-button-icon MuiButton-endIcon" aria-hidden="true">{props.endIcon}</span>}</>);
}
export function IconButton(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('goby-icon-button MuiIconButton-root', props.color && `goby-tone-${props.color}`, props.size === 'small' && 'goby-small'));
  return createElement(props.component ?? (props.href ? 'a' : 'button'), { ...attributes, ref: props.ref, type: props.type ?? (props.href ? undefined : 'button') }, content);
}
export function Divider(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('goby-divider MuiDivider-root', props.orientation === 'vertical' && 'goby-divider-vertical'));
  return content ? <div {...attributes} role="separator">{content}</div> : <hr {...attributes} aria-orientation={props.orientation ?? 'horizontal'} />;
}
export function Chip(props: ChipProps) {
  const { attributes, content, t, language, preserveContent } = usePresentation(props, cx('goby-chip MuiChip-root', `goby-tone-${props.color ?? 'default'}`, props.size === 'small' && 'goby-small', props.variant === 'outlined' && 'goby-chip-outlined'));
  const label = props.label === undefined ? content : preserveContent ? props.label : translateNode(props.label, language);
  return <span {...attributes}>{props.icon && <span className="goby-chip-icon" aria-hidden="true">{props.icon}</span>}<span className="MuiChip-label">{label}</span>{props.onDelete && <button className="goby-chip-delete" type="button" disabled={props.disabled} aria-label={t(`Remove ${plainText(props.label)}`)} onClick={props.onDelete}>{props.deleteIcon ?? '×'}</button>}</span>;
}
export function Avatar(props: UIProps) {
  const { attributes, content, t, preserveContent } = usePresentation(props, 'goby-avatar MuiAvatar-root');
  return <span {...attributes}>{props.src ? <img src={props.src} alt={preserveContent ? props.alt ?? '' : t(props.alt ?? '')} /> : content}</span>;
}
export function InputAdornment(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('goby-input-adornment MuiInputAdornment-root', props.position === 'end' && 'goby-adornment-end'));
  return <span {...attributes}>{content}</span>;
}

const FormContext = createContext<{ disabled?: boolean; labelId?: string }>({});
export function TextField(props: TextFieldProps) {
  const autoId = useId();
  const id = props.id ?? `field-${autoId}`;
  const { attributes, t, language, preserveContent } = usePresentation(props, cx('goby-field MuiFormControl-root MuiTextField-root', props.error && 'Mui-error', props.disabled && 'Mui-disabled', props.fullWidth && 'goby-full-width', props.multiline && 'goby-multiline', props.select && 'goby-select-field'));
  const form = useContext(FormContext);
  const disabled = props.disabled ?? form.disabled;
  const html = { ...props.inputProps, ...props.slotProps?.htmlInput };
  const inputOptions = { ...props.InputProps, ...props.slotProps?.input };
  const selectOptions = props.slotProps?.select ?? props.SelectProps ?? {};
  const helperId = props.helperText ? `${id}-helper-text` : undefined;
  const describedBy = [html['aria-describedby'], selectOptions['aria-describedby'], helperId].filter(Boolean).join(' ') || undefined;
  const inputAttributes: Record<string, any> = {
    ...html, id, name: props.name, disabled, required: props.required, value: props.value ?? '', defaultValue: props.defaultValue,
    onChange: props.onChange, onFocus: props.onFocus, onBlur: props.onBlur, onKeyDown: props.onKeyDown, autoFocus: props.autoFocus,
    autoComplete: props.autoComplete ?? html.autoComplete, placeholder: typeof props.placeholder === 'string' && !preserveContent ? t(props.placeholder) : props.placeholder,
    readOnly: inputOptions.readOnly ?? props.readOnly ?? html.readOnly, 'aria-invalid': props.error || undefined, 'aria-describedby': describedBy,
    'aria-label': typeof html['aria-label'] === 'string' && !preserveContent ? t(html['aria-label']) : html['aria-label'],
    ref: (node: any) => {
      if (node) node.autofocus = Boolean(props.autoFocus);
      setRef(props.inputRef, node); setRef(inputOptions.ref, node); setRef(html.ref, node);
    },
  };
  if (props.type === 'file') {
    delete inputAttributes.value;
    delete inputAttributes.defaultValue;
  } else if (props.defaultValue !== undefined && props.value === undefined) delete inputAttributes.value;
  const label = preserveContent ? props.label : translateNode(props.label, language);
  const options = nativeOptions(props.children, language, preserveContent);
  for (const key of ['id', 'name', 'value', 'defaultValue', 'required', 'disabled', 'autoFocus', 'autoComplete', 'placeholder', 'onChange', 'onFocus', 'onBlur', 'onKeyDown', 'type', 'rows', 'readOnly']) delete attributes[key];
  return <div {...attributes}>
    {label != null && <label className={cx('goby-field-label MuiInputLabel-root', props.error && 'Mui-error', disabled && 'Mui-disabled')} htmlFor={id}>{label}{props.required && <span aria-hidden="true"> *</span>}</label>}
    <div className={cx('goby-input-wrap MuiOutlinedInput-root', props.error && 'Mui-error', disabled && 'Mui-disabled')}>
      {inputOptions.startAdornment}
      {props.select ? <select {...inputAttributes} multiple={props.multiple} className="goby-input MuiSelect-select">{options}</select>
        : props.multiline ? <textarea {...inputAttributes} rows={props.rows ?? props.minRows ?? 3} style={{ maxHeight: props.maxRows ? `${props.maxRows * 1.5 + 1}em` : undefined, ...html.style }} className="goby-input MuiInputBase-input" />
          : <input {...inputAttributes} type={props.type ?? 'text'} className="goby-input MuiInputBase-input" />}
      {inputOptions.endAdornment}
    </div>
    {props.helperText != null && <div id={helperId} {...props.FormHelperTextProps} {...props.slotProps?.formHelperText} className={cx('goby-field-help MuiFormHelperText-root', props.error && 'Mui-error')} aria-live={props.error ? 'polite' : props.slotProps?.formHelperText?.['aria-live']}>{preserveContent ? props.helperText : translateNode(props.helperText, language)}</div>}
  </div>;
}
export function FormControl(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('goby-form-control MuiFormControl-root', props.fullWidth && 'goby-full-width'));
  return <FormContext.Provider value={{ disabled: props.disabled }}>{createElement(props.component ?? 'div', attributes, content)}</FormContext.Provider>;
}
export function FormLabel(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-field-label MuiFormLabel-root');
  return createElement(props.component ?? 'div', attributes, content);
}
export function InputLabel(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-field-label MuiInputLabel-root');
  return <label {...attributes}>{content}</label>;
}
export function OutlinedInput(props: UIProps) { return <TextField {...props} />; }
export function FormControlLabel(props: UIProps) {
  const { attributes, language, preserveContent } = usePresentation(props, cx('goby-control-label MuiFormControlLabel-root', props.disabled && 'Mui-disabled'));
  const control = isValidElement<UIProps>(props.control) ? cloneElement(props.control, { disabled: props.control.props.disabled ?? props.disabled }) : props.control;
  const label = preserveContent ? props.label : translateNode(props.label, language);
  return <label {...attributes}>{props.labelPlacement === 'start' && <span className="MuiFormControlLabel-label">{label}</span>}{control}{props.labelPlacement !== 'start' && <span className="MuiFormControlLabel-label">{label}</span>}</label>;
}
const RadioContext = createContext<{ name?: string; value?: any; onChange?: UIProps['onChange'] }>({});
function CheckControl({ kind, ...props }: UIProps & { kind: 'checkbox' | 'radio' | 'switch' }) {
  const { attributes, t, preserveContent } = usePresentation(props, cx(`goby-check-control goby-${kind}`, `Mui${kind === 'switch' ? 'Switch' : kind === 'radio' ? 'Radio' : 'Checkbox'}-root`, props.disabled && 'Mui-disabled'));
  const radio = useContext(RadioContext);
  const form = useContext(FormContext);
  const input = props.slotProps?.input ?? props.inputProps ?? {};
  const id = useId();
  const ref = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => { if (ref.current) ref.current.indeterminate = Boolean(props.indeterminate); }, [props.indeterminate]);
  const checked = kind === 'radio' && props.checked === undefined ? radio.value === props.value : props.checked;
  const disabled = props.disabled ?? form.disabled;
  const inputProps = { ...input, type: kind === 'radio' ? 'radio' : 'checkbox', id: props.id ?? input.id ?? id, disabled, checked, defaultChecked: props.defaultChecked, name: props.name ?? radio.name, value: props.value, onChange: (event: React.ChangeEvent<HTMLInputElement>) => { props.onChange?.(event, event.target.checked); if (kind === 'radio') radio.onChange?.(event, event.target.value); }, role: kind === 'switch' ? 'switch' : undefined, 'aria-checked': props.indeterminate ? 'mixed' as const : checked, 'aria-label': input['aria-label'] ? preserveContent ? input['aria-label'] : t(input['aria-label']) : attributes['aria-label'], ref: (node: HTMLInputElement | null) => { ref.current = node; setRef(props.inputRef, node); } };
  for (const key of ['checked', 'defaultChecked', 'name', 'value', 'disabled', 'id', 'onChange', 'indeterminate', 'inputRef', 'labelPlacement', 'control']) delete attributes[key];
  return <span {...attributes}><input {...inputProps} /><span className={`goby-${kind}-visual`} aria-hidden="true">{kind === 'checkbox' && (props.indeterminate ? '−' : '✓')}</span></span>;
}
export function Checkbox(props: UIProps) { return <CheckControl {...props} kind="checkbox" />; }
export function Switch(props: UIProps) { return <CheckControl {...props} kind="switch" />; }
export function Radio(props: UIProps) { return <CheckControl {...props} kind="radio" />; }
export function RadioGroup(props: UIProps) {
  const id = useId();
  const { attributes, content } = usePresentation(props, 'goby-radio-group');
  delete attributes.value; delete attributes.onChange;
  return <RadioContext.Provider value={{ name: props.name ?? id, value: props.value, onChange: props.onChange }}><div {...attributes} role="radiogroup">{content}</div></RadioContext.Provider>;
}

const DialogContext = createContext<{ onClose?: UIProps['onClose'] }>({});
const dialogLastFocus = new WeakMap<HTMLDialogElement, HTMLElement>();
function currentModal(exclude?: HTMLDialogElement) {
  return Array.from(document.querySelectorAll<HTMLDialogElement>('dialog[open]')).filter((item) => item !== exclude && item.matches(':modal')).at(-1);
}
function restoreDialogFocus(dialog: HTMLDialogElement, target: HTMLElement | null) {
  const activeModal = currentModal(dialog);
  const valid = (element: HTMLElement | null | undefined): element is HTMLElement => Boolean(element?.isConnected && element !== document.body && element !== dialog && !dialog.contains(element) && !element.matches(':disabled') && !element.closest('[inert]') && (!activeModal || activeModal.contains(element)));
  const candidates = [target, activeModal ? dialogLastFocus.get(activeModal) : null, document.activeElement as HTMLElement | null];
  const next = candidates.find(valid);
  if (next) next.focus({ preventScroll: true });
  else if (activeModal) (activeModal.querySelector<HTMLElement>('[autofocus],button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href],[tabindex="0"]') ?? activeModal).focus({ preventScroll: true });
}
export function Dialog(props: UIProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  // React commits descendant autoFocus before layout effects, so capture the
  // trigger while rendering the opening transition, before portal commit.
  if (props.open && !ref.current?.open) {
    const active = document.activeElement;
    if (active instanceof HTMLElement && active !== document.body && !ref.current?.contains(active)) returnFocus.current = active;
  }
  const onClose = useRef(props.onClose);
  onClose.current = props.onClose;
  const { attributes, content } = usePresentation(props, cx('goby-dialog MuiDialog-paper', props.maxWidth === 'xs' || props.role === 'alertdialog' || props.centered || props['data-layout'] === 'modal' ? 'goby-dialog-center' : 'goby-dialog-sheet', props.fullScreen && 'goby-dialog-fullscreen'));
  const paperStyle = useSx(props.slotProps?.paper?.sx ?? props.PaperProps?.sx);
  useLayoutEffect(() => {
    const dialog = ref.current;
    if (!dialog || !props.open) return;
    if (!dialog.open) dialog.showModal();
    const rememberFocus = (event: FocusEvent) => { if (event.target instanceof HTMLElement) dialogLastFocus.set(dialog, event.target); };
    dialog.addEventListener('focusin', rememberFocus);
    if (document.activeElement instanceof HTMLElement && dialog.contains(document.activeElement)) dialogLastFocus.set(dialog, document.activeElement);
    const request = (event: Event) => { if (event.target !== dialog) return; event.preventDefault(); if (!props.disableEscapeKeyDown) onClose.current?.(event, 'escapeKeyDown'); };
    dialog.addEventListener('cancel', request);
    return () => { dialog.removeEventListener('cancel', request); dialog.removeEventListener('focusin', rememberFocus); if (dialog.open) dialog.close(); if (!props.disableRestoreFocus) restoreDialogFocus(dialog, returnFocus.current); };
  }, [props.open, props.disableEscapeKeyDown, props.disableRestoreFocus]);
  if (!props.open) return null;
  for (const key of ['open', 'onClose', 'centered']) delete attributes[key];
  const maxWidth = props.fullScreen ? '100vw' : props.maxWidth === 'xs' ? 440 : props.maxWidth === 'md' ? 760 : props.maxWidth === 'lg' ? 960 : props.maxWidth === 'xl' ? 1200 : 560;
  return createPortal(<dialog {...attributes} ref={ref} aria-modal="true" style={{ ...attributes.style, '--dialog-width': typeof maxWidth === 'number' ? `${maxWidth}px` : maxWidth, ...paperStyle } as CSSProperties} onClick={(event) => { props.onClick?.(event); if (event.target !== event.currentTarget) return; const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) props.onClose?.(event, 'backdropClick'); }}><DialogContext.Provider value={{ onClose: props.onClose }}><div className="goby-dialog-layout">{content}</div></DialogContext.Provider></dialog>, document.body);
}
export function DialogTitle(props: UIProps) {
  const { attributes, content, t } = usePresentation(props, 'goby-dialog-title MuiDialogTitle-root');
  const context = useContext(DialogContext);
  const headingStyle: Record<string, unknown> = {};
  const titleStyle = { ...attributes.style };
  for (const key of Object.keys(titleStyle)) if (key.startsWith('padding')) { headingStyle[key] = titleStyle[key]; delete titleStyle[key]; }
  return <header className="goby-dialog-heading" style={headingStyle as CSSProperties}><h2 {...attributes} style={titleStyle}>{content}</h2>{context.onClose && <button type="button" className="goby-icon-button goby-dialog-close" aria-label={t('Close dialog')} onClick={(event) => context.onClose?.(event, 'closeButton')}><CloseIcon /></button>}</header>;
}
export function DialogContent(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-dialog-content MuiDialogContent-root');
  return <div {...attributes}>{content}</div>;
}
export function DialogActions(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-dialog-actions MuiDialogActions-root');
  return <footer {...attributes}>{content}</footer>;
}
function CloseIcon() { return <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>; }

const MenuContext = createContext(false);
export function Menu(props: UIProps) {
  const ref = useRef<HTMLDivElement>(null);
  const { attributes, content } = usePresentation(props, 'goby-menu MuiMenu-paper');
  const paperStyle = useSx(props.slotProps?.paper?.sx ?? props.PaperProps?.sx);
  useLayoutEffect(() => {
    if (!props.open || !ref.current) return;
    const anchor = props.anchorEl;
    ref.current.querySelector<HTMLElement>('[role="menuitem"]:not([disabled])')?.focus();
    const outside = (event: PointerEvent) => { if (!ref.current?.contains(event.target as Node) && !anchor?.contains(event.target as Node)) props.onClose?.(event, 'backdropClick'); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.preventDefault(); props.onClose?.(event, 'escapeKeyDown'); } if (event.key === 'Tab') props.onClose?.(event, 'tabKeyDown'); };
    document.addEventListener('pointerdown', outside); document.addEventListener('keydown', key);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', key); if (anchor?.isConnected) anchor.focus({ preventScroll: true }); };
  }, [props.open]);
  if (!props.open) return null;
  const rect = props.anchorEl?.getBoundingClientRect();
  const top = rect ? Math.min(rect.bottom + 5, window.innerHeight - 80) : 56;
  const left = rect ? Math.max(8, Math.min(rect.left, window.innerWidth - 250)) : 8;
  delete attributes.open; delete attributes.onClose;
  return createPortal(<MenuContext.Provider value><div {...attributes} ref={ref} role="menu" style={{ ...attributes.style, top, left, ...paperStyle }} onKeyDown={(event) => { if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return; const items = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('[role="menuitem"]:not([disabled])')); const current = items.indexOf(document.activeElement as HTMLElement); const index = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (current + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length; event.preventDefault(); items[index]?.focus(); }}>{content}</div></MenuContext.Provider>, document.body);
}
export function MenuItem(props: UIProps) {
  const isMenu = useContext(MenuContext);
  const { attributes, content } = usePresentation(props, cx('goby-menu-item MuiMenuItem-root', props.selected && 'Mui-selected'));
  if (!isMenu) return <option value={props.value} disabled={props.disabled}>{plainText(content)}</option>;
  delete attributes.selected;
  return createElement(props.component ?? 'button', { ...attributes, type: 'button', role: props.role ?? 'menuitem', tabIndex: props.disabled ? -1 : 0 }, content);
}
export function Tooltip(props: UIProps) {
  const { t, language } = useTranslation();
  const id = useId();
  const [visible, setVisible] = useState(false);
  const child = isValidElement<UIProps>(props.children) ? cloneElement(props.children, { 'aria-describedby': [props.children.props['aria-describedby'], visible ? id : undefined].filter(Boolean).join(' ') || undefined }) : props.children;
  return <span className="goby-tooltip-anchor" onMouseEnter={() => setVisible(true)} onMouseLeave={() => setVisible(false)} onFocus={() => setVisible(true)} onBlur={() => setVisible(false)} onKeyDown={(event) => { if (event.key === 'Escape' && visible) { event.preventDefault(); event.stopPropagation(); setVisible(false); } }}>{child}{visible && props.title && <span id={id} role="tooltip" className="goby-tooltip">{preservesTranslation(props) ? props.title : typeof props.title === 'string' ? t(props.title) : translateNode(props.title, language)}</span>}</span>;
}

const TableSection = createContext<'head' | 'body'>('body');
export function TableContainer(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-table-container MuiTableContainer-root');
  return createElement(props.component ?? 'div', attributes, content);
}
export function Table(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('goby-table MuiTable-root', props.size === 'small' && 'goby-table-small'));
  return <table {...attributes}>{content}</table>;
}
export function TableHead(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'MuiTableHead-root');
  return <TableSection.Provider value="head"><thead {...attributes}>{content}</thead></TableSection.Provider>;
}
export function TableBody(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'MuiTableBody-root');
  return <TableSection.Provider value="body"><tbody {...attributes}>{content}</tbody></TableSection.Provider>;
}
export function TableRow(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('MuiTableRow-root', props.selected && 'Mui-selected'));
  delete attributes.hover; delete attributes.selected;
  return <tr {...attributes}>{content}</tr>;
}
export function TableCell(props: UIProps) {
  const section = useContext(TableSection);
  const { attributes, content } = usePresentation(props, cx('MuiTableCell-root', section === 'head' && 'MuiTableCell-head'), { textAlign: props.align });
  return createElement(props.component ?? (section === 'head' ? 'th' : 'td'), { scope: section === 'head' ? 'col' : undefined, ...attributes }, content);
}
interface PaginationProps extends UIProps {
  count: number; page: number; rowsPerPage: number;
  onPageChange: (event: AnyEvent, page: number) => void;
  onRowsPerPageChange?: (event: React.ChangeEvent<HTMLInputElement>) => void;
  labelDisplayedRows?: (info: { from: number; to: number; count: number; page: number }) => ReactNode;
}
export function TablePagination(props: PaginationProps) {
  const { attributes, t, language } = usePresentation(props, 'goby-pagination MuiTablePagination-root');
  const from = props.count === 0 ? 0 : props.page * props.rowsPerPage + 1;
  const to = props.count === -1 ? (props.page + 1) * props.rowsPerPage : Math.min(props.count, (props.page + 1) * props.rowsPerPage);
  const options = props.rowsPerPageOptions ?? [10, 25, 50];
  return <div {...attributes}><div className="MuiTablePagination-toolbar">{options.length > 1 && <label>{t(props.labelRowsPerPage ?? 'Rows per page:')}<select value={props.rowsPerPage} disabled={props.disabled} onChange={(event) => props.onRowsPerPageChange?.(event as unknown as React.ChangeEvent<HTMLInputElement>)}>{options.map((entry: number | { label: string; value: number }) => <option key={typeof entry === 'number' ? entry : entry.value} value={typeof entry === 'number' ? entry : entry.value}>{typeof entry === 'number' ? entry : t(entry.label)}</option>)}</select></label>}<span className="goby-pagination-count">{props.labelDisplayedRows ? translateNode(props.labelDisplayedRows({ from, to, count: props.count, page: props.page }), language) : `${from}–${to} / ${props.count}`}</span><div className="goby-pagination-buttons">{props.showFirstButton && <button type="button" className="goby-icon-button" aria-label={t('Go to first page')} disabled={props.disabled || props.page === 0} onClick={(event) => props.onPageChange(event, 0)}>«</button>}<button type="button" className="goby-icon-button" aria-label={t('Go to previous page')} disabled={props.disabled || props.page === 0} onClick={(event) => props.onPageChange(event, props.page - 1)}>‹</button><button type="button" className="goby-icon-button" aria-label={t('Go to next page')} disabled={props.disabled || props.count !== -1 && to >= props.count} onClick={(event) => props.onPageChange(event, props.page + 1)}>›</button>{props.showLastButton && <button type="button" className="goby-icon-button" aria-label={t('Go to last page')} disabled={props.disabled || to >= props.count} onClick={(event) => props.onPageChange(event, Math.max(0, Math.ceil(props.count / props.rowsPerPage) - 1))}>»</button>}</div></div></div>;
}

const TabsContext = createContext<{ value: any; onChange?: UIProps['onChange']; id: string }>({ value: 0, id: '' });
export function Tabs(props: UIProps) {
  const id = useId();
  const { attributes, content } = usePresentation(props, 'goby-tabs MuiTabs-root');
  delete attributes.value; delete attributes.onChange;
  return <TabsContext.Provider value={{ value: props.value, onChange: props.onChange, id }}><div {...attributes} role="tablist" onKeyDown={(event) => { props.onKeyDown?.(event); if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return; const tabs = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('[role="tab"]:not([disabled])')); const current = tabs.indexOf(document.activeElement as HTMLElement); const index = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (current + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length; event.preventDefault(); tabs[index]?.focus(); tabs[index]?.click(); }}><div className="MuiTabs-list">{Children.map(content, (child, index) => isValidElement<UIProps>(child) ? cloneElement(child, { value: child.props.value ?? index }) : child)}</div></div></TabsContext.Provider>;
}
export function Tab(props: UIProps) {
  const tabs = useContext(TabsContext);
  const selected = props.value === tabs.value;
  const { attributes, language, preserveContent } = usePresentation(props, cx('goby-tab MuiTab-root', selected && 'Mui-selected'));
  const component = props.component ?? 'button';
  delete attributes.value;
  return createElement(component, { ...attributes, role: 'tab', type: component === 'button' ? 'button' : undefined, tabIndex: selected ? 0 : -1, 'aria-selected': selected, onClick: (event: React.MouseEvent) => { props.onClick?.(event); if (!event.defaultPrevented || component === 'a') tabs.onChange?.(event, props.value); } }, <>{props.icon && <span className="MuiTab-icon" aria-hidden="true">{props.icon}</span>}{preserveContent ? props.label : translateNode(props.label, language)}</>);
}
const ToggleContext = createContext<{ value: any; exclusive?: boolean; disabled?: boolean; onChange?: UIProps['onChange'] }>({ value: null });
export function ToggleButtonGroup(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-segmented MuiToggleButtonGroup-root');
  delete attributes.value; delete attributes.onChange; delete attributes.disabled;
  return <ToggleContext.Provider value={{ value: props.value, exclusive: props.exclusive, onChange: props.onChange, disabled: props.disabled }}><div {...attributes} role="group">{content}</div></ToggleContext.Provider>;
}
export function ToggleButton(props: UIProps) {
  const group = useContext(ToggleContext);
  const selected = props.selected ?? (Array.isArray(group.value) ? group.value.includes(props.value) : group.value === props.value);
  const { attributes, content } = usePresentation(props, cx('goby-segment MuiToggleButton-root', selected && 'Mui-selected'));
  delete attributes.value; delete attributes.selected;
  return <button {...attributes} type="button" disabled={props.disabled ?? group.disabled} aria-pressed={selected} onClick={(event) => { props.onClick?.(event); const value = group.exclusive ? selected ? null : props.value : selected ? (group.value ?? []).filter((entry: unknown) => entry !== props.value) : [...(group.value ?? []), props.value]; group.onChange?.(event, value); }}>{content}</button>;
}

export function Alert(props: UIProps) {
  const { attributes, content, t } = usePresentation(props, cx('goby-alert MuiAlert-root', `goby-tone-${props.severity ?? 'success'}`, props.variant === 'filled' && 'goby-alert-filled'));
  return <div {...attributes} role={props.role ?? (props.severity === 'error' ? 'alert' : 'status')}><span className="goby-alert-icon MuiAlert-icon" aria-hidden="true">{props.icon === false ? null : props.icon ?? (props.severity === 'success' ? '✓' : props.severity === 'error' ? '!' : 'i')}</span><div className="goby-alert-message MuiAlert-message">{content}</div>{props.action}{props.onClose && <button type="button" className="goby-icon-button goby-small" aria-label={t('Close alert')} onClick={(event) => props.onClose?.(event)}><CloseIcon /></button>}</div>;
}
export function Snackbar(props: UIProps) {
  const { attributes, content, language, preserveContent } = usePresentation(props, 'goby-snackbar MuiSnackbar-root');
  const onClose = useRef(props.onClose); onClose.current = props.onClose;
  useEffect(() => { if (!props.open || !props.autoHideDuration) return; const timer = setTimeout(() => onClose.current?.(undefined, 'timeout'), props.autoHideDuration); return () => clearTimeout(timer); }, [props.open, props.autoHideDuration, props.message]);
  if (!props.open) return null;
  delete attributes.open; delete attributes.message; delete attributes.onClose;
  return createPortal(<div {...attributes} role={props.children ? undefined : 'status'}>{content ?? <span>{preserveContent ? props.message : translateNode(props.message, language)}</span>}{props.action}</div>, document.body);
}
export function Collapse(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-collapse MuiCollapse-root');
  if (!props.in) return null;
  return <div {...attributes}>{content}</div>;
}
export function Skeleton(props: UIProps) {
  const { attributes } = usePresentation(props, cx('goby-skeleton MuiSkeleton-root', props.variant === 'circular' && 'goby-skeleton-circular', props.variant === 'text' && 'goby-skeleton-text'), { width: props.width, height: props.height ?? (props.variant === 'text' ? 20 : 32) });
  delete attributes.width; delete attributes.height; delete attributes.animation;
  return <span {...attributes} aria-hidden="true" />;
}
export function CircularProgress(props: UIProps) {
  const size = props.size ?? 32;
  const { attributes, t } = usePresentation(props, 'goby-spinner MuiCircularProgress-root', { width: size, height: size, color: props.color === 'inherit' ? 'inherit' : themeColor(props.color ? `${props.color}.main` : 'primary.main') });
  delete attributes.thickness; delete attributes.value;
  return <span {...attributes} role="progressbar" aria-label={attributes['aria-label'] ?? t('Loading')} aria-valuenow={props.variant === 'determinate' ? props.value : undefined}><svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth={props.thickness ?? 2} strokeDasharray={props.variant === 'determinate' ? `${56.5 * (props.value ?? 0) / 100} 56.5` : '38 57'} strokeLinecap="round" /></svg></span>;
}
export function LinearProgress(props: UIProps) {
  const { attributes, t } = usePresentation(props, cx('goby-progress MuiLinearProgress-root', `goby-tone-${props.color ?? 'primary'}`, props.variant !== 'determinate' && 'goby-progress-indeterminate'));
  delete attributes.value; delete attributes.valueBuffer;
  return <div {...attributes} role="progressbar" aria-label={attributes['aria-label'] ?? t('Progress')} aria-valuemin={0} aria-valuemax={100} aria-valuenow={props.variant === 'determinate' ? props.value : undefined}><span className="goby-progress-bar MuiLinearProgress-bar MuiLinearProgress-bar1" style={{ width: props.variant === 'determinate' ? `${Math.max(0, Math.min(100, props.value ?? 0))}%` : '35%' }} /></div>;
}

const AccordionContext = createContext<{ expanded: boolean; toggle: (event: AnyEvent) => void; id: string }>({ expanded: false, toggle: () => undefined, id: '' });
export function Accordion(props: UIProps) {
  const [open, setOpen] = useState(props.defaultExpanded ?? false);
  const expanded = props.expanded ?? open;
  const id = useId();
  const { attributes, content } = usePresentation(props, cx('goby-accordion MuiAccordion-root', expanded && 'Mui-expanded'));
  delete attributes.onChange;
  return <AccordionContext.Provider value={{ expanded, id, toggle: (event) => { setOpen(!expanded); props.onChange?.(event, !expanded); } }}><section {...attributes}>{content}</section></AccordionContext.Provider>;
}
export function AccordionSummary(props: UIProps) {
  const context = useContext(AccordionContext);
  const { attributes, content } = usePresentation(props, 'goby-accordion-summary MuiAccordionSummary-root');
  return <button {...attributes} type="button" id={props.id ?? `${context.id}-summary`} aria-expanded={context.expanded} aria-controls={props['aria-controls'] ?? `${context.id}-content`} onClick={context.toggle}><span>{content}</span><span className="goby-accordion-chevron" aria-hidden="true">{props.expandIcon ?? '⌄'}</span></button>;
}
export function AccordionDetails(props: UIProps) {
  const context = useContext(AccordionContext);
  const { attributes, content } = usePresentation(props, 'goby-accordion-details MuiAccordionDetails-root');
  return <div {...attributes} id={props.id ?? `${context.id}-content`} role="region" aria-labelledby={`${context.id}-summary`} hidden={!context.expanded}>{content}</div>;
}
export function List(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-list MuiList-root');
  return createElement(props.component ?? 'ul', attributes, content);
}
export function ListItemButton(props: UIProps) {
  const { attributes, content } = usePresentation(props, cx('goby-list-button MuiListItemButton-root', props.selected && 'Mui-selected'));
  delete attributes.selected;
  return createElement(props.component ?? 'button', { ...attributes, type: props.component ? undefined : 'button' }, content);
}
export function ListItemIcon(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-list-icon MuiListItemIcon-root');
  return <span {...attributes}>{content}</span>;
}
export function ListItemText(props: UIProps) {
  const { attributes, language, preserveContent } = usePresentation(props, 'goby-list-text MuiListItemText-root');
  return <span {...attributes}><span className="MuiListItemText-primary">{preserveContent ? props.primary : translateNode(props.primary, language)}</span>{props.secondary && <span className="MuiListItemText-secondary">{preserveContent ? props.secondary : translateNode(props.secondary, language)}</span>}</span>;
}
export function Link(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-link MuiLink-root');
  return createElement(props.component ?? 'a', attributes, content);
}
export function Breadcrumbs(props: UIProps) {
  const { attributes, content } = usePresentation(props, 'goby-breadcrumbs MuiBreadcrumbs-root');
  delete attributes.separator;
  return <nav {...attributes}><ol>{Children.toArray(content).map((child, i) => <li key={i}>{i > 0 && <span className="goby-breadcrumb-separator" aria-hidden="true">{props.separator ?? '›'}</span>}{child}</li>)}</ol></nav>;
}

interface SelectProps<T> extends UIProps { value?: T; onChange?: (event: { target: { value: T | string } }) => void; renderValue?: (value: T) => ReactNode; }
export function Select<T = unknown>(props: SelectProps<T>) {
  const { attributes, language, preserveContent } = usePresentation(props, 'goby-standalone-select MuiSelect-root');
  const autoId = useId();
  const id = props.id ?? autoId;
  const label = props.labelId ? undefined : props.label ?? (isValidElement<UIProps>(props.input) ? props.input.props.label : undefined);
  const values = props.multiple ? (Array.isArray(props.value) ? props.value : []) : props.value;
  for (const key of ['input', 'value', 'onChange', 'multiple']) delete attributes[key];
  return <div {...attributes}>{label != null && <label htmlFor={id} className="goby-field-label">{preserveContent ? label : translateNode(label, language)}</label>}<select id={id} className="goby-input MuiSelect-select" multiple={props.multiple} value={values as string | string[]} disabled={props.disabled} aria-labelledby={props.labelId} aria-label={attributes['aria-label']} onChange={(event) => { const next = props.multiple ? Array.from(event.currentTarget.selectedOptions, (option) => option.value) : event.target.value; props.onChange?.({ target: { value: next as T } }); }}>{nativeOptions(props.children, language, preserveContent)}</select></div>;
}

interface AutocompleteProps<T> extends Omit<UIProps, 'onChange'> {
  options: readonly T[];
  value?: any;
  inputValue?: string;
  getOptionLabel?: (option: T) => string;
  isOptionEqualToValue?: (option: T, value: T) => boolean;
  onChange?: (event: AnyEvent, value: any, reason: string) => void;
  onInputChange?: (event: AnyEvent, value: string, reason: string) => void;
  renderInput: (params: TextFieldProps) => ReactNode;
  renderTags?: (value: T[], getTagProps: (options: { index: number }) => UIProps) => ReactNode;
  renderValue?: (value: T[], getItemProps: (options: { index: number }) => UIProps) => ReactNode;
  filterOptions?: (options: T[], state: { inputValue: string; getOptionLabel: (option: T) => string }) => T[];
}
export function Autocomplete<T = string>(props: AutocompleteProps<T>) {
  const { t } = useTranslation();
  const [input, setInput] = useState('');
  const [open, setOpen] = useState(false);
  const [highlight, setHighlight] = useState<{ key: string; index: number } | null>(null);
  const host = useRef<HTMLDivElement>(null);
  const listbox = useRef<HTMLDivElement>(null);
  const autoId = useId();
  const id = props.id ?? autoId;
  const inputValue = props.inputValue ?? input;
  const label = (value: T | string) => typeof value === 'string' ? value : props.getOptionLabel?.(value as T) ?? String(value);
  const options = props.filterOptions ? props.filterOptions([...props.options], { inputValue, getOptionLabel: label }) : props.options.filter((option) => label(option).toLocaleLowerCase().includes(inputValue.toLocaleLowerCase()));
  const visibleOptions = props.loading ? [] : options.slice(0, 100);
  const optionKey = JSON.stringify([inputValue, visibleOptions.map(label)]);
  const active = highlight?.key === optionKey && highlight.index >= 0 && highlight.index < visibleOptions.length ? highlight.index : -1;
  const current = props.multiple ? (Array.isArray(props.value) ? props.value : []) : props.value;
  const preserve = preservesTranslation(props);
  const choose = (event: AnyEvent, value: T | string, reason = 'selectOption') => {
    if (props.multiple) { const next = [...current, value]; props.onChange?.(event, next, reason); setInput(''); props.onInputChange?.(event, '', 'reset'); }
    else { props.onChange?.(event, value, reason); setInput(label(value)); props.onInputChange?.(event, label(value), 'reset'); }
    setOpen(false); setHighlight(null);
  };
  useEffect(() => { setHighlight(null); }, [optionKey]);
  useLayoutEffect(() => {
    if (!open || props.disabled || active < 0) return;
    const list = listbox.current;
    const option = list?.querySelectorAll<HTMLElement>('[role="option"]')[active];
    if (!list || !option) return;
    const listBounds = list.getBoundingClientRect();
    const optionBounds = option.getBoundingClientRect();
    const top = listBounds.top + list.clientTop;
    const bottom = top + list.clientHeight;
    // Scroll only the popup's own viewport; the input and page stay in place.
    if (optionBounds.top < top) list.scrollTop -= top - optionBounds.top;
    else if (optionBounds.bottom > bottom) list.scrollTop += optionBounds.bottom - bottom;
  }, [active, open, optionKey, props.disabled]);
  useEffect(() => { if (!props.multiple && props.inputValue === undefined) setInput(current ? label(current) : ''); }, [props.value]);
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => { if (!host.current?.contains(event.target as Node)) { setOpen(false); setHighlight(null); } };
    document.addEventListener('pointerdown', outside); return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  const renderTags = props.renderValue ?? props.renderTags;
  const tags = props.multiple && renderTags ? renderTags(current, ({ index }) => ({ key: index, disabled: Boolean(props.disabled), translate: preserve ? 'no' : undefined, onDelete: props.disabled ? undefined : () => props.onChange?.(null, current.filter((_entry: T, i: number) => i !== index), 'removeOption') })) : props.multiple ? current.map((value: T, index: number) => <Chip key={index} label={label(value)} translate={preserve ? 'no' : undefined} size="small" onDelete={props.disabled ? undefined : () => props.onChange?.(null, current.filter((_entry: T, i: number) => i !== index), 'removeOption')} />) : null;
  const params: TextFieldProps = {
    id, disabled: props.disabled, value: inputValue,
    onChange: (event) => { setInput(event.target.value); props.onInputChange?.(event, event.target.value, 'input'); setOpen(true); setHighlight(null); },
    onFocus: (event) => { setOpen(true); setHighlight(null); props.onFocus?.(event); },
    onBlur: props.onBlur,
    onKeyDown: (event) => {
      props.onKeyDown?.(event);
      if (event.defaultPrevented || event.nativeEvent?.isComposing) return;
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault(); setOpen(true);
        const index = visibleOptions.length === 0 ? -1 : event.key === 'ArrowDown' ? active < 0 ? 0 : (active + 1) % visibleOptions.length : active < 0 ? visibleOptions.length - 1 : (active - 1 + visibleOptions.length) % visibleOptions.length;
        setHighlight({ key: optionKey, index });
      } else if (event.key === 'Enter' && (open || props.freeSolo)) {
        event.preventDefault();
        if (open && active >= 0) choose(event, visibleOptions[active]);
        else if (props.freeSolo && inputValue.trim()) choose(event, inputValue.trim(), 'createOption');
      } else if (event.key === 'Escape' && open) {
        event.preventDefault(); event.stopPropagation(); setOpen(false); setHighlight(null);
      }
    },
    slotProps: { htmlInput: { role: 'combobox', 'aria-expanded': open && !props.disabled, 'aria-controls': `${id}-options`, 'aria-autocomplete': 'list', 'aria-activedescendant': open && !props.disabled && active >= 0 ? `${id}-option-${active}` : undefined, autoComplete: 'off' } },
    InputProps: { startAdornment: tags && <span className="goby-autocomplete-tags">{tags}</span> },
  };
  if (!props.disableClearable && !props.disabled && (inputValue || props.multiple && current.length || !props.multiple && current)) {
    params.InputProps.endAdornment = <span className="goby-input-adornment goby-adornment-end"><button type="button" className="goby-icon-button goby-small" aria-label={t('Clear')} onClick={(event) => { setInput(''); setHighlight(null); props.onInputChange?.(event, '', 'clear'); props.onChange?.(event, props.multiple ? [] : null, 'clear'); host.current?.querySelector('input')?.focus(); }}><CloseIcon /></button></span>;
  }
  return <div className={cx('goby-autocomplete', props.className)} style={useSx(props.sx)} ref={host} onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) { setOpen(false); setHighlight(null); } }}>{props.renderInput(params)}{open && !props.disabled && <div ref={listbox} className="goby-autocomplete-options" role="listbox" id={`${id}-options`}>{props.loading ? <div className="goby-autocomplete-empty">{t(props.loadingText ?? 'Loading…')}</div> : visibleOptions.length ? visibleOptions.map((option, index) => <div key={index} id={`${id}-option-${index}`} role="option" aria-selected={active === index} className={cx('goby-autocomplete-option', active === index && 'is-active')} onPointerDown={(event) => event.preventDefault()} onPointerMove={() => setHighlight({ key: optionKey, index })} onClick={(event) => choose(event, option)}>{label(option)}</div>) : <div className="goby-autocomplete-empty">{t(props.noOptionsText ?? (props.freeSolo && inputValue ? 'Press Enter to add' : 'No options'))}</div>}</div>}</div>;
}
