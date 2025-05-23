use dbus::arg::Append;
use dbus::blocking::{BlockingSender, Connection};
use dbus::channel::Sender;
use dbus::message::Message;
use std::error::Error;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::{Duration, Instant};

use crate::constant;

pub type ListenCallback = Box<dyn Fn(Message) + Send + Sync>;

/// Sends a signal and waits for an acknowledgement from the same object/interface
/// whose member name is the original `member` plus `_ack`.
/// If the ack is not received within `ACK_TIMEOUT`, the signal is resent.
/// The operation is attempted up to `MAX_RETRIES` times.
pub fn send_signal(
    object_path: &str,
    interface: &str,
    member: &str,
    payload: &str,
) -> Result<(), Box<dyn Error + Send + Sync>> {
    let conn = Connection::new_session()?;

    // Listen for the expected ack
    let ack_member = format!("{}_ack", member);
    let rule = format!(
        "type='signal',interface='{}',member='{}',path='{}'",
        interface, ack_member, object_path
    );
    conn.add_match_no_cb(&rule)?;

    // Send the signal up to `MAX_RETRIES` times
    let max_retries = constant::DBUS_MAX_RETRIES;
    let ack_timeout = Duration::from_millis(constant::DBUS_ACK_TIMEOUT);
    for attempt in 0..max_retries {
        // Send the signal
        let msg = Message::new_signal(object_path, interface, member)?.append1(payload);
        if conn.send(msg).is_err() {
            eprintln!(
                "DBUS: Failed to send signal (attempt {}/{})",
                attempt + 1,
                max_retries
            );
        }

        // Wait for the ack until the timeout expires
        let deadline = Instant::now() + ack_timeout;
        while Instant::now() < deadline {
            let remaining = deadline - Instant::now();
            if let Some(reply) = conn.channel().blocking_pop_message(remaining)? {
                // Confirm we received the correct ack from the intended object
                if reply.member().map(|m| m.to_string()) == Some(ack_member.clone())
                    && reply.path().map(|p| p.to_string()) == Some(object_path.to_string())
                {
                    return Ok(()); // Ack received
                }
            }
        }

        // If we didn't receive the ack, log an error
        eprintln!(
            "DBUS: Ack '{}' not received within {:?} (attempt {}/{}) – retrying…",
            ack_member,
            ack_timeout,
            attempt + 1,
            max_retries
        );
    }

    Err(format!(
        "DBUS: Ack '{}' not received after {} attempts",
        ack_member, max_retries
    )
    .into())
}

/// Waits up to `timeout_ms` milliseconds for a signal, immediately emits
/// a `<member>_ack` signal back to the same object/interface, then returns
/// the payload of the received message.
pub fn receive_signal(
    object_path: &str,
    interface: &str,
    member: &str,
    timeout_ms: u64,
) -> Result<Message, Box<dyn Error + Send + Sync>> {
    let conn = Connection::new_session()?;
    let rule = format!(
        "type='signal',interface='{}',member='{}',path='{}'",
        interface, member, object_path
    );
    println!("Rule: {}", rule);
    conn.add_match_no_cb(&rule)?;

    let end_time = Instant::now() + Duration::from_millis(timeout_ms);
    while Instant::now() < end_time {
        let time_left = end_time - Instant::now();
        if let Ok(msg) = receive_internal(&conn, object_path, interface, member, time_left) {
            return Ok(msg);
        }
    }

    Err(format!("Timed out after {} ms waiting for '{}'", timeout_ms, member).into())
}

/// Waits up to `duration` milliseconds for a signal, immediately emits
/// a `<member>_ack` signal back if received the right signal and returns
/// the payload of the received message.
/// If the signal doesn't match the expected object path or member, an error is returned.
fn receive_internal(
    conn: &Connection,
    object_path: &str,
    interface: &str,
    member: &str,
    duration: Duration,
) -> Result<Message, Box<dyn Error + Send + Sync>> {
    let msg_opt = conn.channel().blocking_pop_message(duration)?;

    let msg = msg_opt.ok_or(format!("DBUS: Do not receive any signal"))?;
    let r_object_path = msg
        .path()
        .map(|p| p.to_string())
        .ok_or(format!("DBUS: Received signal with no path: {:?}", msg))?;
    let r_member = msg
        .member()
        .map(|m| m.to_string())
        .ok_or(format!("DBUS: Received signal with no member: {:?}", msg))?;
    if r_object_path != object_path {
        return Err(format!(
            "DBUS: Received signal from wrong object: {} (expected {})",
            r_object_path, object_path
        )
        .into());
    }
    if r_member != member {
        return Err(format!(
            "DBUS: Received signal with wrong member: {} (expected {})",
            r_member, member
        )
        .into());
    }

    // Send acknowledgement
    println!(
        "DBUS: Sending ack signal '{}_ack' to {}, {}",
        member, object_path, interface
    );
    let mut ack_msg = Message::new_signal(object_path, interface, &format!("{}_ack", member))?;
    ack_msg = ack_msg.append1("");
    if conn.send(ack_msg).is_err() {
        // Failed to send ack signal doesn't matter, just log an error
        eprintln!("DBUS: Failed to send ack signal '{}_ack'", member);
    }

    Ok(msg)
}

pub fn listen_for_signal(
    object_path: &str,
    interface: &str,
    member: &str,
    stop: Arc<AtomicBool>,
    cb: ListenCallback,
) {
    let object_path = object_path.to_string();
    let interface = interface.to_string();
    let member = member.to_string();
    tokio::task::spawn_blocking(move || {
        let conn = Connection::new_session().expect("DBUS: failed to create connection");
        let rule = format!(
            "type='signal',interface='{}',member='{}',path='{}'",
            interface, member, object_path
        );
        conn.add_match_no_cb(&rule)
            .expect("DBUS: failed to add match");

        println!(
            "DBUS: Listening for '{}' signal in a background thread",
            member
        );
        while !stop.load(Ordering::Relaxed) {
            if let Ok(msg) = receive_internal(
                &conn,
                &object_path,
                &interface,
                &member,
                Duration::from_millis(constant::DBUS_LISTEN_WAKE_UP_INTERVAL),
            ) {
                cb(msg);
            }
        }
    });
}

/// Sends a blocking D‑Bus method call and returns the reply `Message`.
///
/// * `destination` – well‑known or unique bus name of the service to call.
/// * `object_path` – object path exposed by that service.
/// * `interface` – interface that defines the method.
/// * `member` – the method (member) name.
/// * `payload` – single string argument to pass to the method.
///
/// The function waits up to `constant::DBUS_ACK_TIMEOUT` milliseconds for the
/// reply and returns the raw `Message` so the caller can extract whatever
/// return values it needs.
pub fn call_method<T: Send + Sync + Append>(
    destination: &str,
    object_path: &str,
    interface: &str,
    member: &str,
    payload: T,
    timeout_ms: u64,
) -> Result<Message, Box<dyn Error + Send + Sync>> {
    // Establish a connection on the session bus
    let conn = Connection::new_session()?;

    // Build the method‑call message and attach the payload
    let mut msg = Message::new_method_call(destination, object_path, interface, member)?;
    msg = msg.append1(payload);

    // Send the message and block until we get the reply (or timeout)
    let reply = conn.send_with_reply_and_block(msg, Duration::from_millis(timeout_ms))?;

    Ok(reply)
}
