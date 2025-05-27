// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

use std::error::Error;
use serde::Deserialize;
use serde::Serialize;
use lens_sdk::StreamOption;
use lens_sdk::option::StreamOption::{Some, None, EndOfStream};

#[link(wasm_import_module = "lens")]
extern "C" {
    fn next() -> *mut u8;
}

// #[derive(Deserialize, Clone)]
// pub struct Log {
//     pub address: String,
//     pub topics: Vec<String>,
//     pub events: [Event::Vec<Event>;],
// }

// #[derive(Deserialize, Clone)]
// pub struct Event {
//     pub eventName: String,
// }

#[derive(Deserialize, Clone)]
pub struct Input {
    pub from: String,
    pub to: String,
    // pub logs: [Log::Vec<Log>],
}


#[derive(Serialize)]
pub struct Output {
    from: String,
    to: String,
    // address: String,
    // eventName: String,
    // topic0: String,
    // topic1: String,
    // topic2: String,
    // topic3: String,
    // topic4: String,
}

#[no_mangle]
pub extern fn alloc(size: usize) -> *mut u8 {
    lens_sdk::alloc(size)
}

#[no_mangle]
pub extern fn transform() -> *mut u8 {
    match try_transform() {
        Ok(o) => match o {
            Some(result_json) => lens_sdk::to_mem(lens_sdk::JSON_TYPE_ID, &result_json),
            None => lens_sdk::nil_ptr(),
            EndOfStream => lens_sdk::to_mem(lens_sdk::EOS_TYPE_ID, &[]),
        },
        Err(e) => lens_sdk::to_mem(lens_sdk::ERROR_TYPE_ID, &e.to_string().as_bytes())
    }
}

fn try_transform() -> Result<StreamOption<Vec<u8>>, Box<dyn Error>> {
    loop {
        let ptr = unsafe { next() };
        let input = match lens_sdk::try_from_mem::<Input>(ptr)? {
            Some(v) => v,
            // Implementations of `transform` are free to handle nil however they like. In this
            // implementation we chose to return nil given a nil input.
            None => return Ok(None),
            EndOfStream => return Ok(EndOfStream)
        };
        // let default_topic = "0x0000000000000000000000000000000000000000000000000000000000000000".to_string();
        // let mut topics: Vec<String> = vec![default_topic.clone(); 5];

        // for (i, topic) in _input.topics.iter().enumerate().take(5) {
        //     topics[i] = topic.clone();
        // }
        let result = Output {
            from: input.from,
            to: input.to,
            // address: _input.logs.address,
            // eventName: _input.eventName,
            // topic0: topics[0].clone(),
            // topic1: topics[1].clone(),
            // topic2: topics[2].clone(),
            // topic3: topics[3].clone(),
            // topic4: topics[4].clone(),
        };
        let result_json = serde_json::to_vec(&result)?;
        lens_sdk::free_transport_buffer(ptr)?;
        return Ok(Some(result_json))
    }
}
