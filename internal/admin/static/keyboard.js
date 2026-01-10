(function() {
  // Keyboard shortcuts mapping
  const SHORTCUTS = {
    'g d': '/admin/',
    'g u': '/admin/users',
    'g o': '/admin/domains',
    'g q': '/admin/queue',
    'g l': '/admin/logs',
    'g s': '/admin/system'
  };

  let keyBuffer = '';
  let keyTimeout = null;
  let selectedRow = -1;

  // Show help dialog
  function showHelp() {
    let dialog = document.getElementById('keyboard-help');
    if (!dialog) {
      dialog = document.createElement('div');
      dialog.id = 'keyboard-help';
      dialog.className = 'keyboard-help-dialog';
      dialog.innerHTML =
        '<div class="keyboard-help-content">' +
        '<h2>keyboard shortcuts</h2>' +
        '<table>' +
        '<tr><th>shortcut</th><th>action</th></tr>' +
        '<tr><td><kbd>g</kbd> <kbd>d</kbd></td><td>go to dashboard</td></tr>' +
        '<tr><td><kbd>g</kbd> <kbd>u</kbd></td><td>go to users</td></tr>' +
        '<tr><td><kbd>g</kbd> <kbd>o</kbd></td><td>go to domains</td></tr>' +
        '<tr><td><kbd>g</kbd> <kbd>q</kbd></td><td>go to queue</td></tr>' +
        '<tr><td><kbd>g</kbd> <kbd>l</kbd></td><td>go to logs</td></tr>' +
        '<tr><td><kbd>g</kbd> <kbd>s</kbd></td><td>go to system</td></tr>' +
        '<tr><td><kbd>r</kbd></td><td>refresh page</td></tr>' +
        '<tr><td><kbd>/</kbd></td><td>focus search</td></tr>' +
        '<tr><td><kbd>j</kbd></td><td>next row</td></tr>' +
        '<tr><td><kbd>k</kbd></td><td>previous row</td></tr>' +
        '<tr><td><kbd>n</kbd></td><td>next page</td></tr>' +
        '<tr><td><kbd>p</kbd></td><td>previous page</td></tr>' +
        '<tr><td><kbd>?</kbd></td><td>show this help</td></tr>' +
        '<tr><td><kbd>Esc</kbd></td><td>close dialogs</td></tr>' +
        '</table>' +
        '<p class="help-footer">press <kbd>Esc</kbd> to close</p>' +
        '</div>';
      dialog.addEventListener('click', function(e) {
        if (e.target === dialog) {
          dialog.style.display = 'none';
        }
      });
      document.body.appendChild(dialog);
    }
    dialog.style.display = 'flex';
  }

  // Select table row with j/k navigation
  function selectRow(direction) {
    const rows = document.querySelectorAll('table tbody tr');
    if (rows.length === 0) return;

    // Clear previous selection
    if (selectedRow >= 0 && selectedRow < rows.length) {
      rows[selectedRow].classList.remove('row-selected');
    }

    // Calculate new selection
    selectedRow += direction;
    if (selectedRow < 0) selectedRow = 0;
    if (selectedRow >= rows.length) selectedRow = rows.length - 1;

    // Apply selection
    rows[selectedRow].classList.add('row-selected');
    rows[selectedRow].scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }

  // Global keydown handler
  document.addEventListener('keydown', function(e) {
    // Ignore keyboard shortcuts when typing in inputs
    const target = e.target;
    if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT') {
      return;
    }

    // Handle single-key shortcuts
    if (e.key === 'Escape') {
      // Close help dialog
      const dialog = document.getElementById('keyboard-help');
      if (dialog) {
        dialog.style.display = 'none';
      }
      // Clear row selection
      selectedRow = -1;
      document.querySelectorAll('.row-selected').forEach(function(row) {
        row.classList.remove('row-selected');
      });
      return;
    }

    if (e.key === '?') {
      e.preventDefault();
      showHelp();
      return;
    }

    if (e.key === 'r') {
      e.preventDefault();
      window.location.reload();
      return;
    }

    if (e.key === '/') {
      e.preventDefault();
      // Focus first text input or search field
      const input = document.querySelector('input[type="text"], input[type="search"], input[name="username"], input[name="sender"], input[name="name"]');
      if (input) {
        input.focus();
        input.select();
      }
      return;
    }

    if (e.key === 'j') {
      e.preventDefault();
      selectRow(1);
      return;
    }

    if (e.key === 'k') {
      e.preventDefault();
      selectRow(-1);
      return;
    }

    if (e.key === 'n') {
      e.preventDefault();
      const next = document.querySelector('.pagination a[rel="next"]');
      if (next) {
        window.location.href = next.href;
      }
      return;
    }

    if (e.key === 'p') {
      e.preventDefault();
      const prev = document.querySelector('.pagination a[rel="prev"]');
      if (prev) {
        window.location.href = prev.href;
      }
      return;
    }

    // Handle two-key shortcuts (g+d, g+u, etc.)
    keyBuffer += e.key.toLowerCase();

    // Clear buffer after 1 second
    clearTimeout(keyTimeout);
    keyTimeout = setTimeout(function() {
      keyBuffer = '';
    }, 1000);

    // Check if buffer matches a shortcut
    const url = SHORTCUTS[keyBuffer];
    if (url) {
      window.location.href = url;
      keyBuffer = '';
    }
  });
})();
