(function() {
  const POLL_INTERVAL = 10000; // 10 seconds
  let timeoutId = null;
  let lastUpdate = Date.now();

  function formatTime(isoString) {
    const date = new Date(isoString);
    return date.toLocaleString('en-US', {
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false
    });
  }

  function updateStats(data) {
    // Update stat cards
    const usersStat = document.getElementById('stat-users');
    const domainsStat = document.getElementById('stat-domains');
    const messagesStat = document.getElementById('stat-messages');
    const queueStat = document.getElementById('stat-queue');
    const uptimeStat = document.getElementById('stat-uptime');

    if (usersStat) usersStat.textContent = data.users;
    if (domainsStat) domainsStat.textContent = data.domains;
    if (messagesStat) messagesStat.textContent = data.messages;

    // Update queue stat (show pending count)
    if (queueStat && data.queue) {
      queueStat.textContent = data.queue.pending;
    }

    // Update uptime stat
    if (uptimeStat && data.uptime) {
      uptimeStat.textContent = data.uptime;
    }

    // Update recent activity table
    updateRecentActivity(data.recent_auth, data.recent_delivery);

    // Update timestamp
    lastUpdate = Date.now();
    const updateTime = document.getElementById('last-update');
    if (updateTime) {
      updateTime.textContent = 'updated just now';
    }

    // Show live indicator
    const liveIndicator = document.getElementById('live-indicator');
    if (liveIndicator) {
      liveIndicator.style.display = 'inline';
    }
  }

  function updateRecentActivity(authLogs, deliveryLogs) {
    const tbody = document.querySelector('#recent-activity tbody');
    if (!tbody) return;

    const rows = [];

    // Add auth logs
    if (authLogs && authLogs.length > 0) {
      authLogs.forEach(function(log) {
        const statusBadge = log.success
          ? '<span class="badge badge-success">success</span>'
          : '<span class="badge badge-danger">failed</span>';
        rows.push({
          time: new Date(log.time),
          html: '<tr><td>' + formatTime(log.time) + '</td><td>auth</td><td>' +
                escapeHtml(log.username) + '</td><td>' + statusBadge + '</td></tr>'
        });
      });
    }

    // Add delivery logs
    if (deliveryLogs && deliveryLogs.length > 0) {
      deliveryLogs.forEach(function(log) {
        let statusBadge;
        if (log.status === 'delivered') {
          statusBadge = '<span class="badge badge-success">delivered</span>';
        } else if (log.status === 'bounced' || log.status === 'rejected') {
          statusBadge = '<span class="badge badge-danger">' + log.status + '</span>';
        } else if (log.status === 'deferred') {
          statusBadge = '<span class="badge badge-warning">deferred</span>';
        } else {
          statusBadge = '<span class="badge badge-secondary">' + escapeHtml(log.status) + '</span>';
        }
        const detail = escapeHtml(log.from.substring(0, 30)) + ' → ' +
                       escapeHtml(log.to.substring(0, 30));
        rows.push({
          time: new Date(log.time),
          html: '<tr><td>' + formatTime(log.time) + '</td><td>delivery</td><td>' +
                detail + '</td><td>' + statusBadge + '</td></tr>'
        });
      });
    }

    // Sort by time (newest first) and take top 10
    rows.sort(function(a, b) { return b.time - a.time; });
    const topRows = rows.slice(0, 10);

    // Update table
    tbody.innerHTML = topRows.map(function(r) { return r.html; }).join('');
  }

  function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }

  function fetchStats() {
    fetch('/admin/api/stats')
      .then(function(response) {
        if (!response.ok) {
          throw new Error('Failed to fetch stats');
        }
        return response.json();
      })
      .then(function(data) {
        updateStats(data);
      })
      .catch(function(err) {
        console.error('Stats update failed:', err);
        const liveIndicator = document.getElementById('live-indicator');
        if (liveIndicator) {
          liveIndicator.style.display = 'none';
        }
      });
  }

  function startPolling() {
    fetchStats();
    timeoutId = setTimeout(function poll() {
      fetchStats();
      timeoutId = setTimeout(poll, POLL_INTERVAL);
    }, POLL_INTERVAL);
  }

  function stopPolling() {
    if (timeoutId) {
      clearTimeout(timeoutId);
      timeoutId = null;
    }
    const liveIndicator = document.getElementById('live-indicator');
    if (liveIndicator) {
      liveIndicator.style.display = 'none';
    }
  }

  // Stop polling when tab is hidden (save resources)
  document.addEventListener('visibilitychange', function() {
    if (document.hidden) {
      stopPolling();
    } else {
      startPolling();
    }
  });

  // Start on page load
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', startPolling);
  } else {
    startPolling();
  }
})();
