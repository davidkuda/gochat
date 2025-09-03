function handleWebSocketMessage(event) {
	scrollToBottom();
}

function scrollToBottom() {
	const container = document.getElementById("messagesContainer");
	container.scrollTop = container.scrollHeight;
}

document.addEventListener("htmx:wsAfterMessage", function (event) {
	scrollToBottom();
});

document.addEventListener("htmx:wsOpen", function (event) {
	console.log("WebSocket connection opened");
});

document.addEventListener("htmx:wsClose", function (event) {
	console.log("WebSocket connection closed");
});

document.addEventListener("htmx:wsError", function (event) {
	console.error("WebSocket error:", event.detail);
});
