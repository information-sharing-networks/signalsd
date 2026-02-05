// Toggles visibility of user vs service account selectors based on account type selection

document.addEventListener('DOMContentLoaded', function() {
	const accountTypeSelect = document.getElementById('account-type');
	if (!accountTypeSelect) {
		return;
	}

	const userSelector = document.getElementById('user-selector');
	const serviceAccountSelector = document.getElementById('service-account-selector');
	const placeholder = document.getElementById('account-placeholder');

	function toggleAccountSelector() {
		// Hide all selectors
		if (userSelector) userSelector.style.display = 'none';
		if (serviceAccountSelector) serviceAccountSelector.style.display = 'none';
		if (placeholder) placeholder.style.display = 'none';

		// Show appropriate selector based on account type
		const accountType = accountTypeSelect.value;
		if (accountType === 'user') {
			if (userSelector) userSelector.style.display = 'block';
		} else if (accountType === 'service-account') {
			if (serviceAccountSelector) serviceAccountSelector.style.display = 'block';
		} else {
			if (placeholder) placeholder.style.display = 'block';
		}
	}

	// Set initial state
	toggleAccountSelector();

	// Listen for changes
	accountTypeSelect.addEventListener('change', toggleAccountSelector);
});

