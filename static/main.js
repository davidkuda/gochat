(function mobileUX() {
	const root = document.documentElement;
	const msgs = () => document.getElementById("messagesContainer");
	const inputBar = () => document.querySelector(".input-container");

	function setAppHeight() {
		const vv = window.visualViewport;
		const h = vv ? vv.height : window.innerHeight;
		root.style.setProperty("--app-height", h + "px");
	}

	function setInputHeightVar() {
		const el = inputBar();
		if (!el) return;
		root.style.setProperty(
			"--input-h",
			el.getBoundingClientRect().height + "px",
		);
	}

	function nearBottom(container, threshold = 120) {
		return (
			container.scrollHeight - container.scrollTop - container.clientHeight <
			threshold
		);
	}

	function scrollToBottom(force = false) {
		const m = msgs();
		if (!m) return;
		if (force || nearBottom(m)) {
			m.scrollTop = m.scrollHeight;
		}
	}

	// Initial measurements
	window.addEventListener("load", () => {
		setAppHeight();
		setInputHeightVar();
		scrollToBottom(true);
	});

	// Update on viewport changes (keyboard open/close, orientation)
	if (window.visualViewport) {
		visualViewport.addEventListener("resize", () => {
			setAppHeight();
			setInputHeightVar();
		});
	}
	window.addEventListener("orientationchange", () => {
		setAppHeight();
		setInputHeightVar();
	});
	window.addEventListener("resize", () => {
		setAppHeight();
		setInputHeightVar();
	});

	// Recompute input height if fonts/styles load later
	document.fonts &&
		document.fonts.addEventListener &&
		document.fonts.addEventListener("loadingdone", setInputHeightVar);

	// Auto-scroll on new messages from HTMX swaps
	document.body.addEventListener("htmx:afterSwap", (e) => {
		if (
			e.detail &&
			e.detail.target &&
			e.detail.target.id === "messagesContainer"
		) {
			scrollToBottom(false);
		}
	});

	// Also when WS connects and on initial content
	document.body.addEventListener("htmx:wsAfterMessage", () =>
		scrollToBottom(false),
	);

	// Expose a tiny API if you need it elsewhere
	window.chatView = { scrollToBottom };
})();

// User data
let userData = {
	name: "",
	gender: "",
	avatar: "",
	user_id: null,
};

// HTMX event handlers
function handleLoginResponse(event) {
	console.log("I get logged");
	const response = event.detail.xhr.response;

	if (event.detail.xhr.status === 200) {
		const data = JSON.parse(response);

		// Store user data from server response
		userData.name = data.name;
		userData.gender = data.gender;
		userData.avatar = data.name.charAt(0).toUpperCase();
		userData.user_id = data.user_id;

		// Set hidden user_id field for messages
		document.getElementById("userId").value = userData.user_id;

		// Hide login screen and show chat screen
		document.getElementById("loginScreen").style.display = "none";
		document.getElementById("chatScreen").style.display = "block";

		// Focus on message input
		document.getElementById("messageInput").focus();
		scrollToBottom();
	} else {
		// Handle login error
		document.getElementById("loginResponse").innerHTML =
			'<div style="color: #ef4444; font-size: 0.875rem; margin-bottom: 1rem;">Login failed. Please try again.</div>';
	}
}

function clearMessageInput() {
	document.getElementById("messageInput").value = "";
	scrollToBottom();
}

// WebSocket message handler (HTMX ws extension will call this)
function handleWebSocketMessage(event) {
	// This is called automatically by HTMX ws extension
	// New messages from websocket will be automatically added to messagesContainer
	scrollToBottom();

	// Add animation class to new messages
	const newMessages = document.querySelectorAll(".message:not(.animated)");
	newMessages.forEach((msg) => {
		msg.classList.add("new", "animated");
		setTimeout(() => {
			msg.classList.remove("new");
		}, 300);
	});
}

// Utility functions
function scrollToBottom() {
	const container = document.getElementById("messagesContainer");
	if (container) {
		container.scrollTop = container.scrollHeight;
	}
}

// User count updates (can be updated via websocket)
function updateUserCount(count) {
	const userCountElement = document.getElementById("userCount");
	if (userCountElement && count) {
		userCountElement.textContent = `${count} users online`;
	}
}

// HTMX event listeners
document.addEventListener("htmx:wsOpen", function (event) {
	console.log("WebSocket connection opened");
});

document.addEventListener("htmx:wsClose", function (event) {
	console.log("WebSocket connection closed");
});

document.addEventListener("htmx:wsError", function (event) {
	console.error("WebSocket error:", event.detail);
});

document.addEventListener("htmx:wsAfterMessage", function (event) {
	handleWebSocketMessage(event);
});

// Focus on name input when page loads
document.addEventListener("DOMContentLoaded", function () {
	const nameInput = document.getElementById("nameInput");
	if (nameInput) {
		nameInput.focus();
	}
});

// Handle form submission preventDefault for HTMX forms
document.addEventListener("htmx:configRequest", function (event) {
	// Add any additional headers or configuration here
	if (event.detail.path === "/api/messages" && userData.user_id) {
		event.detail.headers["X-User-ID"] = userData.user_id;
	}
});
