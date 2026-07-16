try {
  if (window.localStorage.getItem("track-slash.sidebar.collapsed") === "true") {
    document.documentElement.setAttribute("data-sidebar-collapsed", "");
  }
} catch (_) {}
