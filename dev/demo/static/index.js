let _uid;
let _token;
let _conn;
let _kickoff;

// server config.
let _serverConf;

let _eventMap = new Map();
let _sortedEventList = [];  // order by seq DESC.

window.setInterval(requestGetReadStates, 30 * 1000);

function handleHeadSeq(seq) {
	requestGetEvents(seq);
}

function handleGetEventsResp(resp) {
	if (!resp) {
		return;
	}

	if (!resp.events) {
		return;
	}

	for (let e of resp.events) {
		_eventMap.set(e.seq, e);
	}

	// regenerate `_sortedEventList`: sort by seq desc.
	_sortedEventList = [];
	for (let value of _eventMap.values()) {
		_sortedEventList.push(value);
	}
	_sortedEventList.sort(function (a, b) {
		if (a.seq < b.seq) {
			return 1;
		}
		if (a.seq > b.seq) {
			return -1;
		}
		return 0;
	});

	console.log("total local events: %d", _eventMap.size);

	redrawEventTable();
}

function redrawEventTable() {
	let table = document.getElementById('eventListTable');
	let rows = table.rows.length;
	for (let i = 0; i < rows; i++) {
		table.deleteRow(0);
	}

	// show latest 10 events.
	let n = _sortedEventList.length;
	if (n > 10) {
		n = 10;
	}
	let headEvents = [];
	for (i = 0; i < n; i++) {
		headEvents.push(_sortedEventList[i]);
	}

	for (i = 0; i < headEvents.length; i++) {
		let e = headEvents[i];
		let row = table.insertRow(i);
		let cell1 = row.insertCell(0);
		let cell2 = row.insertCell(1);
		if (e.read_state) {
			cell1.innerHTML = '<span class=\'black-dot\'></span>';
			cell2.innerText = 'Event #' + e.seq;
		} else {
			cell1.innerHTML = '<span class=\'red-dot\'></span>';
			cell2.innerHTML = '<a href=\'#' + e.seq +
				'\' onclick=\'return onEventClicked(' + e.seq + ');\'>Event #' +
				e.seq + '</a>';
		}
	}
}

function base64ToByteArray(base64) {
	let binaryStr = window.atob(base64);
	let n = binaryStr.length;
	let byteArray = new Uint8Array(n);
	for (let i = 0; i < n; i++) {
		byteArray[i] = binaryStr.charCodeAt(i);
	}
	return byteArray;
}

function byteArrayToBoolArray(byteArray) {
	let numBytes = byteArray.length;
	let boolArray = new Array(numBytes * 8);
	for (i = 0; i < numBytes; i++) {
		let b = byteArray[i];
		for (let j = 0; j < 8; j++) {
			let value = false;
			if (b & (1 << (7 - j))) {  // big endian
				value = true;
			}
			boolArray[i * 8 + j] = value;
		}
	}
	return boolArray;
}

function handleGetReadStatesResp(resp) {
	if (!resp) {
		return;
	}

	let blocks = resp.blocks;
	if (!blocks || blocks.length == 0) {
		return;
	}

	let changed;

	for (let block of blocks) {
		let startSeq = block.seq;
		let bytes = base64ToByteArray(block.base64);
		let decodedBools = byteArrayToBoolArray(bytes).slice(0, block.len);

		for (let i = 0; i < decodedBools.length; i++) {
			let seq = startSeq - i;
			let e = _eventMap.get(seq);
			if (!e) {
				console.log("event not found: seq: %d", seq);
				continue
			}

			if (e.read_state === undefined) {
				e.read_state = false;
			}

			let read = decodedBools[i];

			if (read != e.read_state) {
				e.read_state = read;
				changed = true
			}
		}
	}
	if (changed) {
		redrawEventTable();
	}
}

function handleStatsResp(resp) {
	if (!resp) {
		return;
	}

	_serverConf = resp.conf

	requestGetEvents(resp.head_seq);
}

function toggleLogin(show) {
	let loginDiv = document.getElementById('loginDiv');
	let dataDiv = document.getElementById('dataDiv');

	if (show) {
		loginDiv.style.display = 'block';
		dataDiv.style.display = 'none';
	} else {
		loginDiv.style.display = 'none';
		dataDiv.style.display = 'block';
	}
}

function handleSetReadResp(resp) {
	if (!resp) {
		return;
	}

	let e = _eventMap.get(resp.seq);
	if (e) {
		if (!e.read_state) {
			e.read_state = true;
			redrawEventTable();
		}
	}
}

function login() {
	let ele = document.getElementsByName('uid');
	for (i = 0; i < ele.length; i++) {
		if (ele[i].checked) {
			_uid = ele[i].value;
		}
	}

	if (!_uid) {
		alert('please select an user');
		return false;
	} else {
		_token = "some-token";
		console.log("uid: %s, token: %s", _uid, _token);

		document.cookie = 'x-uid=' + _uid + '; path=/';
		document.cookie = 'x-token=' + _token + '; path=/';

		toggleLogin(false);
		connectWs();
		return false;
	}
}

function sendIm() {
	let ele = document.getElementsByName('e2e-uid');
	for (i = 0; i < ele.length; i++) {
		if (ele[i].checked) {
			to_uid = ele[i].value;
		}
	}

	if (!to_uid) {
		alert('please select a recipient');
	}
	let e2e = document.getElementById('e2eText').value;
	if (e2e === "") {
		alert('e2e text can not be empty');
	}
	let msg = {
		e2e: {
			from_uid: parseInt(_uid),
			to_uids: [parseInt(to_uid)],
			me2ee: "text/plain",
			body: e2e,
		}
	};
	send(msg);
}

function requestStats() {
	let msg = {
		stats: {
			count_unread: true
		}
	};
	send(msg);
}

function requestSetRead(seq) {
	let msg = { set_read: { seq: seq } };
	send(msg);
}

function requestGetReadStates() {
	if (!_sortedEventList || _sortedEventList.length == 0) {
		return;
	}

	let msg = { get_read_states: { head_seq: _sortedEventList[0].seq } };
	send(msg);
}

function requestGetSessions() {
	let msg = { get_sessions: {} };
	send(msg);
}

function requestGetEvents(seq) {
	if (!_serverConf) {
		return;
	}

	let le2eit = _serverConf.get_events_le2eit;
	let minSeq = 0;
	if (_sortedEventList && _sortedEventList.length > 0) {
		minSeq = _sortedEventList[0].seq;
	}

	console.log("requestGetEvents: fetched head seq %d, new seq %d", minSeq, seq)
	if (seq <= minSeq) {
		return;
	}

	// order by seq desc.

	let maxSeq = seq;
	for (let upper = maxSeq; upper >= 1; upper -= le2eit) {
		let lower = upper - le2eit + 1;
		if (lower <= minSeq) {
			lower = minSeq + 1
		}
		if (lower > upper) {
			break
		}

		let msg = {
			get_events: {
				from_seq: lower,
				to_seq: upper
			},
		};
		send(msg);
	}
}

function send(msg) {
	let s = JSON.stringify(msg);

	if (!_conn || _conn.readyState === WebSocket.CLOSING || _conn.readyState === WebSocket.CLOSED) {
		console.log("error send %s: closing or closed", s);
		if (_kickoff) {
			console.log("skip auto reconnect because kickoff");
		} else {
			console.log("auto reconnect ...");
			connectWs();
		}
		return
	}

	try {
		console.log("send: " + s);
		_conn.send(s);
	} catch (e) {
		console.log("error send %s, error: %s", s, e);
	}
}

function onEventClicked(seq) {
	requestSetRead(seq);
	return true;
}

function stop() {
	if (_conn) {
		_conn.close();
		_conn = undefined;
	}
}

function connectWs() {
	if (!_uid) {
		console.log('uid is empty');
		return;
	}

	let info = document.getElementById('info');
	if (!window['WebSocket']) {
		info.innerHTML =
			'<b>Your browser is too old to support window[\'WebSockets\']</b>';
		return;
	}

	WebSocket.onerror = function (event) {
		console.error('WebSocket error observed:', event);
	};
	_conn = new WebSocket('ws://' + document.location.host + '/ws');
	_conn.onopen = function (evt) {
		info.innerHTML = '<b>connected</b>';
		console.log('connected');
		requestStats();
	};
	_conn.onclose = function (evt) {
		let s = '<b>disconnected</b>';
		if (_kickoff) {
			s += ' by <b>kickoff</b>';
		}
		info.innerHTML = s;
		console.log('disconnected');

		stop();
	};
	_conn.onmessage = function (evt) {
		let maxChars = 150;
		let data = '' + evt.data;
		if (data.length > maxChars) {
			data = data.substring(0, maxChars) + ' ...';
		}
		info.innerText = data
		console.log('message: ' + evt.data);

		try {
			let resp = JSON.parse(evt.data);

			if (resp.error) {
				console.log(resp.error);
				return;
			}

			if (resp.stats) {
				handleStatsResp(resp.stats);
			} else if (resp.get_read_states) {
				handleGetReadStatesResp(resp.get_read_states);
			} else if (resp.head_seq > 0) {
				handleHeadSeq(resp.head_seq);
			} else if (resp.get_events) {
				handleGetEventsResp(resp.get_events);
			} else if (resp.kickoff) {
				_kickoff = true;
				console.log('kicked off');
				stop();
				alert('kicked off');
			} else if (resp.set_read) {
				handleSetReadResp(resp.set_read);
			} else {
				console.log('unknown message');
			}
		} catch (e) {
			console.log(e);
		}
	};
}

if (false) {
	// https://developer.mozilla.org/en-US/docs/Web/API/GlobalEventHandlers/onfocus
	// Test passed with IE 11.
	if ("onfocus" in window) {
		window.onfocus = function () {
			console.log("window.focus");
			if (!uid) {
				toggleLogin(true);
			} else if (!conn) {
				connectWs();
			}
		};
	} else {
		info.innerHTML = "<b>Your browser is too old to support window.onfocus().</b>";
	}

	// se2eulate offline
	if ("onblur" in window) {
		window.onblur = function () {
			console.log("window.blur");
			stop();
		};
	} else {
		div.innerHTML = "<b>Your browser is too old to support window.onblur().</b>";
	}
}

window.onload = function () {
	if (!_uid) {
		toggleLogin(true);
	}
};