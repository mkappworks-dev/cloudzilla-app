// field-validation.js — inline field errors for forms marked data-validate, in the same markup the server renders:
// aria-invalid on the control and <p id="<control id>-error" data-field-error> in its [data-field] wrapper.
(() => {
  const errorId = (field) => field.id + '-error';

  const labelText = (field) => {
    const label = field.id && document.querySelector('label[for="' + CSS.escape(field.id) + '"]');
    return label ? label.textContent.replace('*', '').replace(/\(optional\)/i, '').trim() : 'This field';
  };

  const messageFor = (field) => {
    const v = field.validity;
    if (v.valueMissing) return labelText(field) + ' is required.';
    if (v.typeMismatch && field.type === 'email') return 'Enter a valid email address.';
    if (v.tooShort) return 'Use at least ' + field.minLength + ' characters.';
    if (v.patternMismatch && field.dataset.patternMessage) return field.dataset.patternMessage;
    return field.validationMessage;
  };

  const describedBy = (field) => (field.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean);

  const show = (field, msg) => {
    let p = document.getElementById(errorId(field));
    if (!p) {
      p = document.createElement('p');
      p.id = errorId(field);
      p.setAttribute('data-field-error', '');
      p.className = 'mt-1.5 text-xs text-destructive';
      (field.closest('[data-field]') || field.parentElement).appendChild(p);
    }
    p.textContent = msg;
    field.setAttribute('aria-invalid', 'true');
    const ids = describedBy(field);
    if (!ids.includes(p.id)) field.setAttribute('aria-describedby', ids.concat(p.id).join(' '));
  };

  const clear = (field) => {
    document.getElementById(errorId(field))?.remove();
    field.removeAttribute('aria-invalid');
    const ids = describedBy(field).filter((id) => id !== errorId(field));
    if (ids.length) field.setAttribute('aria-describedby', ids.join(' '));
    else field.removeAttribute('aria-describedby');
  };

  const clearAll = (form) => form.querySelectorAll('[aria-invalid="true"]').forEach(clear);

  window.CzFields = { show, clear, clearAll };

  // The browser's own bubble is light in dark mode, names one field and has no text for aria-describedby.
  const prepare = () => document.querySelectorAll('form[data-validate]').forEach((f) => { f.noValidate = true; });

  document.addEventListener('submit', (e) => {
    const form = e.target;
    if (!form.matches || !form.matches('form[data-validate]')) return;
    clearAll(form);
    const invalid = Array.from(form.elements).filter((el) => el.willValidate && !el.validity.valid && el.id);
    if (!invalid.length) return;
    // stopImmediatePropagation keeps htmx's and the identity dialog's submit listeners from seeing a form that failed.
    e.preventDefault();
    e.stopImmediatePropagation();
    invalid.forEach((el) => show(el, messageFor(el)));
    invalid[0].focus();
  }, true);

  const recover = (e) => {
    if (e.target.getAttribute?.('aria-invalid') === 'true') clear(e.target);
  };
  document.addEventListener('input', recover);
  document.addEventListener('change', recover);

  document.addEventListener('DOMContentLoaded', () => {
    prepare();
    document.querySelector('form[data-validate] [aria-invalid="true"]')?.focus();
  });
})();
