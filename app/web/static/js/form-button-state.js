// functionality for disabling buttons on click and enabling on form changes

document.addEventListener('DOMContentLoaded', function() {
	// Disable button on click
	document.querySelectorAll('[data-disable-on-click]').forEach(function(btn) {
		btn.addEventListener('click', function() {
			this.disabled = true;
		});
	});
});

// Enable button when dropdown selection changes
document.addEventListener('change', function(event) {
	const selector = event.target.getAttribute('data-enable-button-on-change');
	if (selector) {
		const btn = document.querySelector(selector);
		if (btn) {
			btn.disabled = false;
		}
	}

	// Clear result container when dropdown changes
	const resultContainerId = event.target.getAttribute('data-clear-result-on-change');
	if (resultContainerId) {
		const container = document.getElementById(resultContainerId);
		if (container) {
			container.innerHTML = '';
		}
	}
});

