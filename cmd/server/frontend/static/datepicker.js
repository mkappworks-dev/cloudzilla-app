// datepicker.js — calendar date logic (window.CzDate) and the `datePicker` Alpine component
// rendered by components.DatePicker.
//
// A date is { y, m, d } with m in 1..12. All arithmetic runs on these numbers and on day
// ordinals, never on Date objects: new Date('YYYY-MM-DD') parses as UTC and shifts the day
// west of Greenwich, and Date maps years 0..99 to 1900..1999.
(() => {
  const MIN_YEAR = 1;
  const MAX_YEAR = 9999;
  const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
  const WEEKDAYS = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

  const isLeap = (y) => (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;

  const daysInMonth = (y, m) => [31, isLeap(y) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][m - 1];

  const parseISODate = (s) => {
    const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || '');
    if (!match) return null;
    const [y, m, d] = match.slice(1).map(Number);
    if (y < MIN_YEAR || m < 1 || m > 12 || d < 1 || d > daysInMonth(y, m)) return null;
    return { y, m, d };
  };

  const pad = (n, width) => String(n).padStart(width, '0');

  const formatISODate = ({ y, m, d }) => `${pad(y, 4)}-${pad(m, 2)}-${pad(d, 2)}`;

  // Day 1 is 0001-01-01 in the proleptic Gregorian calendar.
  const toOrdinal = ({ y, m, d }) => {
    const py = y - 1;
    let n = 365 * py + Math.floor(py / 4) - Math.floor(py / 100) + Math.floor(py / 400);
    for (let i = 1; i < m; i++) n += daysInMonth(y, i);
    return n + d;
  };

  const MAX_ORDINAL = toOrdinal({ y: MAX_YEAR, m: 12, d: 31 });

  const fromOrdinal = (n) => {
    let y = Math.min(MAX_YEAR, Math.max(MIN_YEAR, Math.floor((n - 1) / 365.2425) + 1));
    while (y > MIN_YEAR && toOrdinal({ y, m: 1, d: 1 }) > n) y--;
    while (y < MAX_YEAR && toOrdinal({ y: y + 1, m: 1, d: 1 }) <= n) y++;
    let rest = n - toOrdinal({ y, m: 1, d: 1 }) + 1;
    let m = 1;
    while (rest > daysInMonth(y, m)) rest -= daysInMonth(y, m++);
    return { y, m, d: rest };
  };

  const clampOrdinal = (n) => Math.min(MAX_ORDINAL, Math.max(1, n));

  // 0 is Monday; 0001-01-01 was a Monday.
  const weekday = (date) => (toOrdinal(date) - 1) % 7;

  const addDays = (date, n) => fromOrdinal(clampOrdinal(toOrdinal(date) + n));

  // A day past the end of the target month lands on its last day (Jan 31 + 1 month = Feb 28/29).
  const addMonths = ({ y, m, d }, n) => {
    const total = Math.min(MAX_YEAR * 12 + 11, Math.max(MIN_YEAR * 12, y * 12 + (m - 1) + n));
    const ny = Math.floor(total / 12);
    const nm = (total % 12) + 1;
    return { y: ny, m: nm, d: Math.min(d, daysInMonth(ny, nm)) };
  };

  const compare = (a, b) => toOrdinal(a) - toOrdinal(b);

  const today = () => {
    const now = new Date();
    return { y: now.getFullYear(), m: now.getMonth() + 1, d: now.getDate() };
  };

  const formatShort = ({ y, m, d }) => `${MONTHS[m - 1].slice(0, 3)} ${d}, ${y}`;

  const formatFull = (date) => `${WEEKDAYS[weekday(date)]}, ${MONTHS[date.m - 1]} ${date.d}, ${date.y}`;

  const monthLabel = (y, m) => `${MONTHS[m - 1]} ${y}`;

  // Six Monday-first weeks covering the month; days before 0001-01-01 or after 9999-12-31
  // are null.
  const monthGrid = (y, m) => {
    const first = toOrdinal({ y, m, d: 1 });
    const start = first - weekday({ y, m, d: 1 });
    const weeks = [];
    for (let w = 0; w < 6; w++) {
      const week = [];
      for (let i = 0; i < 7; i++) {
        const n = start + w * 7 + i;
        week.push(n < 1 || n > MAX_ORDINAL ? null : fromOrdinal(n));
      }
      weeks.push(week);
    }
    return weeks;
  };

  window.CzDate = {
    MIN_YEAR, MAX_YEAR, isLeap, daysInMonth, parseISODate, formatISODate, toOrdinal, fromOrdinal,
    weekday, addDays, addMonths, compare, today, formatShort, formatFull, monthLabel, monthGrid,
  };

  // x-show reveals on a later animation frame than $nextTick, so focus retries until shown.
  const focusWhenShown = (find, tries = 5) => {
    const el = find();
    if (el) el.focus();
    if ((!el || document.activeElement !== el) && tries > 0) requestAnimationFrame(() => focusWhenShown(find, tries - 1));
  };

  document.addEventListener('alpine:init', () => {
    Alpine.data('datePicker', () => ({
      value: '',
      placeholder: '',
      open: false,
      view: { y: 1, m: 1 },
      focused: null,
      popStyle: '',
      unbind: null,

      init() {
        this.value = this.$el.dataset.value || '';
        this.placeholder = this.$el.dataset.placeholder || '';
        const follow = () => {
          if (this.open) this.place();
        };
        window.addEventListener('scroll', follow, true);
        window.addEventListener('resize', follow);
        const dialog = this.$el.closest('dialog');
        // Inside a modal <dialog>, Escape that reaches the dialog closes only the calendar.
        const onCancel = (e) => {
          if (!this.open) return;
          e.preventDefault();
          this.close();
        };
        const onClose = () => (this.open = false);
        if (dialog) {
          dialog.addEventListener('cancel', onCancel);
          dialog.addEventListener('close', onClose);
        }
        this.unbind = () => {
          window.removeEventListener('scroll', follow, true);
          window.removeEventListener('resize', follow);
          if (dialog) {
            dialog.removeEventListener('cancel', onCancel);
            dialog.removeEventListener('close', onClose);
          }
        };
      },

      destroy() {
        if (this.unbind) this.unbind();
      },

      get selected() {
        return parseISODate(this.value);
      },

      get label() {
        return this.selected ? formatShort(this.selected) : '';
      },

      get heading() {
        return monthLabel(this.view.y, this.view.m);
      },

      get weeks() {
        const sel = this.selected;
        const now = today();
        // Cells are keyed by grid position so a month change reuses the focused button
        // instead of removing it, which would drop focus to <body> until focusDay runs.
        return monthGrid(this.view.y, this.view.m).map((week, wi) =>
          week.map((date, di) =>
            date === null
              ? { key: `${wi}-${di}`, blank: true, iso: '', full: '', d: '', inMonth: false, selected: false, today: false, focus: false }
              : {
                  ...date,
                  key: `${wi}-${di}`,
                  blank: false,
                  iso: formatISODate(date),
                  full: formatFull(date),
                  inMonth: date.m === this.view.m && date.y === this.view.y,
                  selected: sel !== null && compare(sel, date) === 0,
                  today: compare(now, date) === 0,
                  focus: this.focused !== null && compare(this.focused, date) === 0,
                },
          ),
        );
      },

      toggle() {
        if (this.open) this.close();
        else this.show();
      },

      show() {
        const start = this.selected || today();
        this.focused = start;
        this.view = { y: start.y, m: start.m };
        this.open = true;
        this.$nextTick(() => {
          this.place();
          this.focusDay();
        });
      },

      close(returnFocus = true) {
        if (!this.open) return;
        this.open = false;
        if (returnFocus) this.$refs.trigger.focus();
      },

      // position:fixed keeps the popover out of the modal body's overflow clipping; it flips
      // above the field when there is no room below.
      place() {
        const r = this.$refs.trigger.getBoundingClientRect();
        const pop = this.$refs.popover;
        const height = pop.offsetHeight || 340;
        const width = pop.offsetWidth || 288;
        const below = r.bottom + 4;
        const top = below + height > window.innerHeight && r.top - 4 - height >= 0 ? r.top - 4 - height : below;
        const left = Math.max(8, Math.min(r.left, window.innerWidth - width - 8));
        this.popStyle = `position:fixed;left:${left}px;top:${top}px`;
      },

      focusDay() {
        const iso = formatISODate(this.focused);
        focusWhenShown(() => this.$refs.popover.querySelector(`[data-date="${iso}"]`));
      },

      moveTo(date) {
        this.focused = date;
        this.view = { y: date.y, m: date.m };
        this.$nextTick(() => this.focusDay());
      },

      shiftMonth(n) {
        const next = addMonths({ y: this.view.y, m: this.view.m, d: 1 }, n);
        this.view = { y: next.y, m: next.m };
        if (this.focused) this.focused = addMonths(this.focused, n);
      },

      onGridKey(e) {
        if (!this.focused) return;
        const f = this.focused;
        const moves = {
          ArrowLeft: () => addDays(f, -1),
          ArrowRight: () => addDays(f, 1),
          ArrowUp: () => addDays(f, -7),
          ArrowDown: () => addDays(f, 7),
          PageUp: () => addMonths(f, e.shiftKey ? -12 : -1),
          PageDown: () => addMonths(f, e.shiftKey ? 12 : 1),
          Home: () => addDays(f, -weekday(f)),
          End: () => addDays(f, 6 - weekday(f)),
        };
        if (!moves[e.key]) return;
        e.preventDefault();
        this.moveTo(moves[e.key]());
      },

      select(cell) {
        if (cell.blank) return;
        this.value = formatISODate(cell);
        this.close();
      },

      pickToday() {
        this.value = formatISODate(today());
        this.close();
      },

      clear() {
        this.value = '';
        this.close();
      },
    }));
  });
})();
