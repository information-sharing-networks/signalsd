// Handles button state and password reset form interactions

function initializeUsersFunctionality() {
	// Disable button on click
	const generatePasswordResetBtn = document.getElementById('generate-password-reset-btn');
	if (generatePasswordResetBtn) {
		generatePasswordResetBtn.addEventListener('click', function() {
			this.disabled = true;
		});
	}

	// Enable button and clear results when dropdown selection changes
	document.addEventListener('change', function(event) {
		const generateBtn = document.getElementById('generate-password-reset-btn');
		if (generateBtn) {
			generateBtn.disabled = false;
		}

		if (event.target.id === 'user-dropdown') {
			const container = document.getElementById('result');
			if (container) {
				container.innerHTML = '';
			}
		}
	});
}

// Initialize when DOM is ready
if (document.readyState === 'loading') {
	document.addEventListener('DOMContentLoaded', initializeUsersFunctionality);
} else {
	initializeUsersFunctionality();
}

function initializePasswordResetForm() {
	// Password reset form handler
	const resetForm = document.getElementById('resetForm');
	if (resetForm) {
		resetForm.addEventListener('submit', async function(e) {
			e.preventDefault();

			const password = document.getElementById('new-password').value;
			const confirmPassword = document.getElementById('confirm-password').value;
			const button = document.querySelector('button');

			if (password.length < 11) {
				alert('Password must be at least 11 characters long');
				return;
			}

			if (password != confirmPassword) {
				alert('Passwords do not match');
				return;
			}

			button.disabled = true;
			button.textContent = 'Resetting...';

			try {
				const response = await fetch(window.location.pathname, {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json',
					},
					body: JSON.stringify({
						new_password: password
					})
				});

				if (response.ok) {
					document.body.innerHTML = '<div class="container reset-password"><h1 style="color: #4caf50;">Password Reset Successful</h1><p>Your password has been successfully reset. You can now log in with your new password.</p></div>';
				} else {
					const error = await response.json();
					alert('Error: ' + (error.message || 'Failed to reset password'));
					button.disabled = false;
					button.textContent = 'Reset Password';
				}
			} catch (error) {
				alert('Error: Failed to reset password. Please try again.');
				button.disabled = false;
				button.textContent = 'Reset Password';
			}
		});
	}
}

// Initialize password reset form when DOM is ready
if (document.readyState === 'loading') {
	document.addEventListener('DOMContentLoaded', initializePasswordResetForm);
} else {
	initializePasswordResetForm();
}

