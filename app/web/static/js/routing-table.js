// Toggle a hidden boolean field and update the button's visual state.
// Expects the hidden input to immediately precede the button in the DOM.
// Buttons must carry the data-toggle-bool attribute.
function toggleBoolField(btn) {
	const input = btn.previousElementSibling;
	const newValue = input.value !== 'true';
	input.value = newValue ? 'true' : 'false';
	btn.textContent = input.value;
	btn.setAttribute('aria-pressed', input.value);
	btn.classList.toggle('btn-active', newValue);
}

// Use event delegation so that rows added dynamically via htmx are also covered.
document.addEventListener('click', function(e) {
	const btn = e.target.closest('[data-toggle-bool]');
	if (btn) {
		toggleBoolField(btn);
	}
});
