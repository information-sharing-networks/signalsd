// Handles button state and form interactions for service account pages

document.addEventListener('DOMContentLoaded', function() {
	// Disable button on click
	const reissueBtn = document.getElementById('reissue-btn');
	if (reissueBtn) {
		reissueBtn.addEventListener('click', function() {
			this.disabled = true;
		});
	}
});

// Enable button and clear results when dropdown selection changes
document.addEventListener('change', function(event) {
	const reissueBtn = document.getElementById('reissue-btn');
	if (reissueBtn) {
		reissueBtn.disabled = false;
	}

	if (event.target.id === 'service-account-dropdown') {
		const container = document.getElementById('result');
		if (container) {
			container.innerHTML = '';
		}
	}
});

