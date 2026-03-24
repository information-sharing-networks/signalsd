// Signal search page functionality

// Button click handler for pretty print, modals, and monitor buttons
document.addEventListener('click', function(e) {
	// Pretty print JSON toggle
	const prettyPrintBtn = e.target.closest('.pretty-print-btn');
	if (prettyPrintBtn) {
		const signalId = prettyPrintBtn.getAttribute('data-signal-id');
		if (!signalId) {
			return;
		}
		togglePrettyPrint(signalId);
	}

	// Open and close modals that show the correlated id content
	const openModalBtn = e.target.closest('.open-modal-btn');
	if (openModalBtn) {
		const signalId = openModalBtn.getAttribute('data-correlated-signal-id');
		if (!signalId) {
			return;
		}
		toggleCorrelatedSignalModal(signalId, 'open');
		return;
	}

	const closeModalBtn = e.target.closest('.close-modal-btn');
	if (closeModalBtn) {
		const signalId = closeModalBtn.getAttribute('data-correlated-signal-id');
		if (!signalId) {
			return;
		}
		toggleCorrelatedSignalModal(signalId, 'close');
		return;
	}

	// note that because htmx trigger uses unsafe-eval when adding filters to a periodic poll, and this is blocked by our CSP policy, we need
	// to handle the toggle of polling manually
	const monitorBtn = e.target.closest('.monitor-btn');
    if (monitorBtn) {
        monitorBtn.classList.toggle('monitoring');
        const isMonitoring = monitorBtn.classList.contains('monitoring');

        monitorBtn.textContent = isMonitoring ? 'Stop Monitoring' : 'Monitor';

		// do not poll if there is a modal open
		const isModalOpen = document.querySelector('dialog:modal') !== null;

		// do not poll if the pretty print is open
		const isPrettyPrintOpen = document.querySelector('.json-content.pretty-printed[style*="display: block"]') !== null;
        const timerDiv = document.getElementById('monitor-timer');
        if (isMonitoring && !isModalOpen && !isPrettyPrintOpen) {
            timerDiv.setAttribute('hx-trigger', 'every 10s');
        } else {
            timerDiv.removeAttribute('hx-trigger');
        }
        htmx.process(timerDiv); // re-initialise without eval
    }
});

function togglePrettyPrint(signalId) {
	console.log('togglePrettyPrint', signalId);
	const signalCard = document.getElementById(signalId);
	if (!signalCard) {
		return;
	}
	console.log('signalCard', signalCard);

	const compactJsonElement = signalCard.querySelector('.json-content.compact');
	const ppJsonElement = signalCard.querySelector('.json-content.pretty-printed');

	if (!compactJsonElement || !ppJsonElement) {
		console.log('compact or pp json element not found');
		return;
	}

	if (signalCard.classList.contains('compact')) {
		signalCard.classList.remove('compact');
		signalCard.classList.add('pretty-printed');
		compactJsonElement.style.display = 'none';
		ppJsonElement.style.display = 'block';
	} else {
		signalCard.classList.remove('pretty-printed');
		signalCard.classList.add('compact');
		compactJsonElement.style.display = 'block';
		ppJsonElement.style.display = 'none';
	}
}

function toggleCorrelatedSignalModal(signalId, action) {
	if (!signalId || !action) {
		return;
	}

	const modal = document.querySelector(`#modal-${signalId}.modal`);
	console.log('modal', modal);
	if (!modal) {
		return;
	}

	if (action === 'close') {
		modal.close();
	}
	if (action === 'open') {
		modal.showModal();
	}
}

